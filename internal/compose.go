package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"log/slog"
	"os"
	"slices"
	"strings"
)

// cleanNewLineTabFromString replaces all new lines and tab characters with their escaped versions (ie \n -> \\n) and returns the
// result
func cleanNewLineTabFromString(output string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			output,
			"\n",
			"\\n",
		),
		"\t",
		"\\t",
	)
}

// DoesProjectNeedApplying wraps the dry run docker compose command and will determine whether the given project needs
// to be applied. This is determined based on whether the dry run contains any lines containing 'Running' which
// indicates that docker compose would try and start any containers. This is not a guarantee that the file is in a
// completely applied state, however is the best indicator we have right now
func DoesProjectNeedApplying(project ProcessedDockerComposeFile) (bool, error) {
	// docker compose --dry-run up -p {name} -f {file}
	file, err := os.CreateTemp("", "active.nqkd.yaml")
	if err != nil {
		return false, err
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil {
			slog.Error("Failed to cleanup temp file", "temp", file.Name(), "error", err)
		}
	}()

	err = os.WriteFile(file.Name(), []byte(project.Content), 0666)
	if err != nil {
		return false, err
	}

	command := Run("docker", "compose", "--dry-run", "-p", project.Name, "-f", file.Name(), "up")
	out, err := command.CombinedOutput()
	slog.Debug(
		"command output",
		"cmd",
		command.Args,
		"output",
		cleanNewLineTabFromString(string(out)),
	)
	if err != nil {
		return false, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "end of 'compose up'") {
			continue
		}

		if !strings.Contains(line, "Running") {
			return true, nil
		}
	}

	return false, nil
}

// ApplyCompose will invoke docker compose on the given project file and wait for the result.
func ApplyCompose(project ProcessedDockerComposeFile) error {
	// docker compose -p {name} -f {file} up -d
	file, err := os.CreateTemp("", "active.nqkd.yaml")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil {
			slog.Error("Failed to cleanup temp file", "temp", file.Name(), "error", err)
		}
	}()

	err = os.WriteFile(file.Name(), []byte(project.Content), 0666)
	if err != nil {
		return err
	}

	command := Run("docker", "compose", "-p", project.Name, "-f", file.Name(), "up", "-d")
	out, err := command.CombinedOutput()
	slog.Debug(
		"command output",
		"cmd",
		command.Args,
		"output",
		cleanNewLineTabFromString(string(out)),
		"configuration",
		cleanNewLineTabFromString(project.Content),
	)
	if err != nil {
		return err
	}
	return nil
}

// BindingPortMapping represents a mapping from a container to the host. This contains the port on the container, the
// port it maps to on the host, the ip address it is bound to, and the type as returned by the docker API
type BindingPortMapping struct {
	// ContainerPort is the port exposed on the container
	ContainerPort uint16 `json:"container_port"`
	// HostPort is the port that is assigned to the container port, this can either be a manually assigned port or
	// an ephermeral one
	HostPort uint16 `json:"host_port"`
	// Binding is the IP Address that the port is listening on on the host
	Binding string `json:"binding"`
	// Type is the type of port as returned by the docker API
	Type string `json:"type"`
}

// BindingContainer represents the set of bindings for a single container, identified by its name
type BindingContainer struct {
	// Name is the name of the container on the host
	Name string `json:"name"`
	// Ports is the set of exposed ports
	Ports []BindingPortMapping `json:"ports"`
}

// BindingProject contains a mapping between a single nqk project and the set of containers that have been spawned from
// it
type BindingProject struct {
	// Project is the name of the nqk project
	Project string `json:"project"`
	// Containers is the mapping of container names that are generated as part of this project, and their exposed ports
	// to the host
	Containers map[string]BindingContainer `json:"containers"`
}

// BindingResult is the set of al projects currently processed by nqk and their exposed ports
type BindingResult struct {
	Projects map[string]BindingProject `json:"projects"`
}

// GetBindingsForAllProjects will process every docker compose file given and attempt to extract their currently running
// containers and the ports that are exposed from them as a result, returning the entire set as a tree
func GetBindingsForAllProjects(cli *client.Client, dctx context.Context, projects []ProcessedDockerComposeFile) (BindingResult, error) {
	result := BindingResult{Projects: make(map[string]BindingProject)}
	for _, project := range projects {
		binding, err := GetProjectBinding(cli, dctx, project)
		if err != nil {
			return BindingResult{}, err
		}

		result.Projects[binding.Project] = *binding
	}

	return result, nil
}

// GetProjectBinding will return all the containers that are associated with the compose project file and then find
// their exposed bindings. Container should be returned in a consistent ordering as well as their ports however this
// is not guaranteed, any requirements on sorting should be implemented by the caller
func GetProjectBinding(cli *client.Client, dctx context.Context, project ProcessedDockerComposeFile) (*BindingProject, error) {
	bind := BindingProject{Project: project.Name, Containers: make(map[string]BindingContainer, 0)}

	list, err := cli.ContainerList(dctx, types.ContainerListOptions{
		Filters: filters.NewArgs(
			filters.KeyValuePair{
				Key:   "label",
				Value: "com.docker.compose.project=" + project.Name,
			}),
	})
	slices.SortFunc(list, func(a, b types.Container) int {
		return strings.Compare(a.ID, b.ID)
	})
	if err != nil {
		slog.Error("Failed to list containers as part of project, cannot produce bindings", "project", project.Name, "source", project.Source, "error", err)
		return nil, err
	}

	slog.Debug("Found containers for project", "project", project.Name, "source", project.Source, "container_count", len(list))
	validPorts := make([]types.Port, 0)
	for _, container := range list {
		bindContainer := BindingContainer{Name: container.ID, Ports: make([]BindingPortMapping, 0)}
		portCopy := make([]types.Port, len(container.Ports))
		for i, port := range container.Ports {
			portCopy[i] = port
		}
		slices.SortFunc(portCopy, func(a, b types.Port) int {
			if v := a.PublicPort - b.PublicPort; v != 0 {
				return int(v)
			}
			if v := a.PrivatePort - b.PrivatePort; v != 0 {
				return int(v)
			}
			if v := strings.Compare(a.IP, b.IP); v != 0 {
				return v
			}
			return strings.Compare(a.Type, b.Type)
		})
		slog.Debug("Result of sort", "ports", portCopy)
		for _, port := range portCopy {
			if port.PublicPort != 0 {
				validPorts = append(validPorts, port)
				bindContainer.Ports = append(bindContainer.Ports, BindingPortMapping{
					ContainerPort: port.PrivatePort,
					HostPort:      port.PublicPort,
					Binding:       port.IP,
					Type:          port.Type,
				})
			}
		}

		bind.Containers[bindContainer.Name] = bindContainer
	}

	slog.Debug("Port scan on project", "project", project.Name, "source", project.Source, "ports", len(validPorts))
	return &bind, nil
}

// EventDefinition represents a single event that could be emitted by the docker runtime.
type EventDefinition struct {
	// Matcher is a function which will be run against the status of the event. This is not implemented as a plain
	// comparison because some event statuses contain additional metadata
	Matcher func(status string) bool
	// Type is the exact type which should be associated with the ent
	Type     string
	Meta     string
	TypeMeta string
}

// PrefixMatcher generates a new event definition which will match docker events wih the prefix string in the status
// and the given type.
func PrefixMatcher(prefix string, typeName string) EventDefinition {
	return EventDefinition{
		Matcher: func(status string) bool {
			return strings.HasPrefix(status, prefix)
		},
		Type:     typeName,
		Meta:     prefix,
		TypeMeta: typeName + "." + prefix,
	}
}

var (
	ContainerAttachEvent       = PrefixMatcher("attach", "container")
	ContainerCommitEvent       = PrefixMatcher("commit", "container")
	ContainerCopyEvent         = PrefixMatcher("copy", "container")
	ContainerCreateEvent       = PrefixMatcher("create", "container")
	ContainerDestroyEvent      = PrefixMatcher("destroy", "container")
	ContainerDetachEvent       = PrefixMatcher("detach", "container")
	ContainerDieEvent          = PrefixMatcher("die", "container")
	ContainerExecCreateEvent   = PrefixMatcher("exec_create", "container")
	ContainerExecDetachEvent   = PrefixMatcher("exec_detach", "container")
	ContainerExecDieEvent      = PrefixMatcher("exec_die", "container")
	ContainerExecStartEvent    = PrefixMatcher("exec_start", "container")
	ContainerExportEvent       = PrefixMatcher("export", "container")
	ContainerHealthStatusEvent = PrefixMatcher("health_status", "container")
	ContainerKillEvent         = PrefixMatcher("kill", "container")
	ContainerOomEvent          = PrefixMatcher("oom", "container")
	ContainerPauseEvent        = PrefixMatcher("pause", "container")
	ContainerRenameEvent       = PrefixMatcher("rename", "container")
	ContainerResizeEvent       = PrefixMatcher("resize", "container")
	ContainerRestartEvent      = PrefixMatcher("restart", "container")
	ContainerStartEvent        = PrefixMatcher("start", "container")
	ContainerStopEvent         = PrefixMatcher("stop", "container")
	ContainerTopEvent          = PrefixMatcher("top", "container")
	ContainerUnpauseEvent      = PrefixMatcher("unpause", "container")
	ContainerUpdateEvent       = PrefixMatcher("update", "container")
	ImageDeleteEvent           = PrefixMatcher("delete", "image")
	ImageImportEvent           = PrefixMatcher("import", "image")
	ImageLoadEvent             = PrefixMatcher("load", "image")
	ImagePullEvent             = PrefixMatcher("pull", "image")
	ImagePushEvent             = PrefixMatcher("push", "image")
	ImageSaveEvent             = PrefixMatcher("save", "image")
	ImageTagEvent              = PrefixMatcher("tag", "image")
	ImageUntagEvent            = PrefixMatcher("untag", "image")
	PluginEnableEvent          = PrefixMatcher("enable", "plugin")
	PluginDisableEvent         = PrefixMatcher("disable", "plugin")
	PluginInstallEvent         = PrefixMatcher("install", "plugin")
	PluginRemoveEvent          = PrefixMatcher("remove", "plugin")
	VolumeCreateEvent          = PrefixMatcher("create", "volume")
	VolumeDestroyEvent         = PrefixMatcher("destroy", "volume")
	VolumeMountEvent           = PrefixMatcher("mount", "volume")
	VolumeUnmountEvent         = PrefixMatcher("unmount", "volume")
	NetworkCreateEvent         = PrefixMatcher("create", "network")
	NetworkConnectEvent        = PrefixMatcher("connect", "network")
	NetworkDestroyEvent        = PrefixMatcher("destroy", "network")
	NetworkDisconnectEvent     = PrefixMatcher("disconnect", "network")
	NetworkRemoveEvent         = PrefixMatcher("remove", "network")
	DaemonReloadEvent          = PrefixMatcher("reload", "daemon")
	ServiceCreateEvent         = PrefixMatcher("create", "service")
	ServiceRemoveEvent         = PrefixMatcher("remove", "service")
	ServiceUpdateEvent         = PrefixMatcher("update", "service")
	NodeCreateEvent            = PrefixMatcher("create", "node")
	NodeRemoveEvent            = PrefixMatcher("remove", "node")
	NodeUpdateEvent            = PrefixMatcher("update", "node")
	SecretCreateEvent          = PrefixMatcher("create", "secret")
	SecretRemoveEvent          = PrefixMatcher("remove", "secret")
	SecretUpdateEvent          = PrefixMatcher("update", "secret")
	ConfigCreateEvent          = PrefixMatcher("create", "config")
	ConfigRemoveEvent          = PrefixMatcher("remove", "config")
	ConfigUpdateEvent          = PrefixMatcher("update", "config")
)

// PossibleEvents is all the possible events nqk recognises from the docker runtime
var PossibleEvents = [...]EventDefinition{
	ContainerAttachEvent,
	ContainerCommitEvent,
	ContainerCopyEvent,
	ContainerCreateEvent,
	ContainerDestroyEvent,
	ContainerDetachEvent,
	ContainerDieEvent,
	ContainerExecCreateEvent,
	ContainerExecDetachEvent,
	ContainerExecDieEvent,
	ContainerExecStartEvent,
	ContainerExportEvent,
	ContainerHealthStatusEvent,
	ContainerKillEvent,
	ContainerOomEvent,
	ContainerPauseEvent,
	ContainerRenameEvent,
	ContainerResizeEvent,
	ContainerRestartEvent,
	ContainerStartEvent,
	ContainerStopEvent,
	ContainerTopEvent,
	ContainerUnpauseEvent,
	ContainerUpdateEvent,
	ImageDeleteEvent,
	ImageImportEvent,
	ImageLoadEvent,
	ImagePullEvent,
	ImagePushEvent,
	ImageSaveEvent,
	ImageTagEvent,
	ImageUntagEvent,
	PluginEnableEvent,
	PluginDisableEvent,
	PluginInstallEvent,
	PluginRemoveEvent,
	VolumeCreateEvent,
	VolumeDestroyEvent,
	VolumeMountEvent,
	VolumeUnmountEvent,
	NetworkCreateEvent,
	NetworkConnectEvent,
	NetworkDestroyEvent,
	NetworkDisconnectEvent,
	NetworkRemoveEvent,
	DaemonReloadEvent,
	ServiceCreateEvent,
	ServiceRemoveEvent,
	ServiceUpdateEvent,
	NodeCreateEvent,
	NodeRemoveEvent,
	NodeUpdateEvent,
	SecretCreateEvent,
	SecretRemoveEvent,
	SecretUpdateEvent,
	ConfigCreateEvent,
	ConfigRemoveEvent,
	ConfigUpdateEvent,
}

type DockerEvent struct {
	Type EventDefinition
	Meta map[string]interface{}
}

func SubscribeToDockerEvents(queue chan DockerEvent) error {
	cmd := Run("docker", "events", "--format", "{{json .}}")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	err = cmd.Start()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanLines)

	go func() {
		for scanner.Scan() {
			slog.Debug("Got raw event, trying to parse")

			data := scanner.Bytes()
			var target map[string]interface{}
			err := json.Unmarshal(data, &target)
			if err != nil {
				slog.Error("Failed to parse docker event as JSON due to an error", "error", err, "value", string(data[:]))
				continue
			}

			var typeRaw string
			var statusRaw string

			if v, ok := target["status"]; ok {
				if vs, ok := v.(string); ok {
					statusRaw = vs
				} else {
					slog.Error("Failed to parse result - could not parse status as string", "value", v)
					continue
				}
			} else {
				slog.Error("Failed to parse result - could not find a status entry", "raw", target)
				continue
			}

			if v, ok := target["Type"]; ok {
				if vs, ok := v.(string); ok {
					typeRaw = vs
				} else {
					slog.Error("Failed to parse result - could not parse Type as string", "value", v)
					continue
				}
			} else {
				slog.Error("Failed to parse result - could not find a Type entry", "raw", target)
				continue
			}

			var matching *EventDefinition = nil
			for _, event := range PossibleEvents {
				if event.Matcher(statusRaw) && event.Type == typeRaw {
					matching = &event
					break
				}
			}

			slog.Debug("Event was found to be of type", "type", matching)

			if matching == nil {
				slog.Error("Failed to parse, could not find a matching event type", "type", typeRaw, "status", statusRaw)
				continue
			}

			queue <- DockerEvent{
				Type: *matching,
				Meta: target,
			}
		}
	}()

	return nil
}
