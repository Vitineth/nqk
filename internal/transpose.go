package internal

import (
	"context"
	"errors"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v2"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ProcessedDockerComposeFile represents a docker compose file which has been loaded from disk, potentially with
// modifications applied.
type ProcessedDockerComposeFile struct {
	// The name of the project for which this compose file is associated, this will be passed to the docker compose
	// commands for naming the project
	Name string
	// The actual content of the file as loaded from disk and processed, this should be ready to be executed
	Content string
	// The source path of this compose file
	Source string
}

// nonAlphanumericRegex matches any characters which are not in the range A-Za-z0-9 and space
var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9 ]+`)

// CleanName will remove any non alphanumeric characters and in the event it begins with a number, will prefix it with
// nqkd_
func CleanName(name string) string {
	replaced := strings.ToLower(nonAlphanumericRegex.ReplaceAllString(name, "_"))
	if !unicode.IsLetter(rune(replaced[0])) && !unicode.IsNumber(rune(replaced[0])) {
		return "nqkd_" + replaced
	}
	return replaced
}

// ProcessDockerComposeFile loads the given file from disk, and attempts to deserialise it into a
// ProcessedDockerComposeFile. Included processing includes
//   - Extracting the name: key and using it as the name of the project
//   - Extracting any auto_volumes: from the services and automatically turning them into valid bind mounts which are
//     mounted to /mnt/nqkd/<project name>/<service name>
func ProcessDockerComposeFile(file string) (*ProcessedDockerComposeFile, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	object := make(map[string]interface{})
	err = yaml.UnmarshalStrict(content, object)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	if n, ok := object["name"]; ok {
		if ns, ok := n.(string); ok {
			name = ns
		} else {
			slog.Warn("Couldn't parse name from configuration as it was the wrong type, expected string", "name", n, "file", file)
		}
		delete(object, "name")
	}

	finalName := CleanName(name)
	if n, ok := object["services"]; ok {
		if ns, ok := n.(map[interface{}]interface{}); ok {
			for _, v := range ns {
				if ks, ok := v.(map[interface{}]interface{}); ok {
					if auto, ok := ks["auto_volumes"]; ok {
						if auto_array, ok := auto.([]interface{}); ok {
							asString := make([]string, len(auto_array))
							for i := 0; i < len(asString); i++ {
								if as, ok := auto_array[i].(string); ok {
									asString[i] = as
								} else {
									return nil, errors.New("invalid auto_volumes definition, could not process as string")
								}
							}

							if _, ok := ks["volumes"]; !ok {
								ks["volumes"] = make([]string, 0)
							}

							if vs, ok := ks["volumes"]; ok {
								if volumes, ok := vs.([]interface{}); ok {
									for _, s := range asString {
										toTransform := s
										if strings.HasPrefix(toTransform, "/") {
											for len(toTransform) > 0 && toTransform[0] == '/' {
												toTransform = toTransform[1:]
											}
											if len(toTransform) == 0 {
												return nil, errors.New("invalid auto volume found, could not make a path after clearing slashes")
											}
										}
										ks["volumes"] = append(volumes, "/mnt/nqkd/"+finalName+"/"+CleanName(strings.ReplaceAll(toTransform, "/", "_"))+":"+s)
									}
								} else {
									slog.Error("could not process, volumes didn't have the expected structure, wanted an array", "volumes", vs, "type", reflect.TypeOf(vs))
									return nil, errors.New("could not process, volumes didn't have the expected structure, wanted an array")
								}
							}

							slog.Info("Reprocessed some auto volumes into regular volumes")
							delete(ks, "auto_volumes")
						} else {
							slog.Warn("Service had auto_volumes but could not be parsed as an interface array", "value", auto, "type", reflect.TypeOf(auto))
							return nil, errors.New("invalid definition, could not process auto_volumes")
						}
					}
				} else {
					slog.Warn("Couldn't process value as a map", "value", v, "type", reflect.TypeOf(v))
				}
			}
		} else {
			slog.Warn("Couldn't parse services")
		}

	}

	out, err := yaml.Marshal(object)
	if err != nil {
		return nil, err
	}

	return &ProcessedDockerComposeFile{
		Name:    finalName,
		Content: string(out),
		Source:  file,
	}, nil
}

// NginxProjectBinding is a binding for a single project, including any http specific bindings, and general TCP/UDP
// bindings
type NginxProjectBinding struct {
	// The contents of the nginx config which handle HTTP traffic to and from the service
	HttpContent string
	// The contents of the nginx config which handle normal UDP/TCP traffic to and from the service
	ServiceContent string
}

// GetLabelForPort will attempt to search the label map from a docker container to resolve the most specific labelling
// possible. As both global and local labels are supported, this will try to resolve the portLabel with the $port
// term substituted for the actual port, falling back to the global level if present. Returns nil if neither label is
// found
func GetLabelForPort(labels map[string]string, globalLabel string, portLabel string, port uint16) *string {
	resolvedPortLabel := strings.ReplaceAll(portLabel, "$port", strconv.Itoa(int(port)))
	if value, ok := labels[resolvedPortLabel]; ok {
		return &value
	}

	if value, ok := labels[globalLabel]; ok {
		return &value
	}

	return nil
}

// StringOrElse resolves a string pointer into either its actual value, or a fallback value if it is a nil pointer
func StringOrElse(data *string, fallback string) string {
	if data != nil {
		return *data
	}

	return fallback
}

// GenerateFilesForContainerNginxBinding will generate two files for a given container binding, one for HTTP
// traffic, and one for normal UDP/TCP traffic. This is based off the set of labels defined in constants.go, prefixed
// with Label*. This handles skipping hidden ports, ssl, bind addresses, domain mapping and HTTP vs TCP vs UDP traffic
// to produce the configurations. Each configuration is based on the Nginx* templates in constants.go
func GenerateFilesForContainerNginxBinding(container BindingContainer, labels map[string]string, config BindingConfiguration) (NginxProjectBinding, error) {
	http := ""
	service := ""

	for _, port := range container.Ports {
		hide := StringOrElse(GetLabelForPort(labels, "", LabelPortHide, port.ContainerPort), "false") == "true"
		if hide {
			slog.Debug("Skipping port because it is marked as hidden", "port", port, "container", container.Name)
			continue
		}

		if port.Binding == "::" {
			slog.Debug("Skipping port because its ipv6", "binding", port.Binding, "port", port, "container", container.Name)
			continue
		}

		portType := StringOrElse(GetLabelForPort(labels, LabelGlobalType, LabelPortType, port.ContainerPort), port.Type)
		if (portType == ValueTypeTcp && port.Type == ValueTypeUdp) || (portType == ValueTypeUdp && port.Type == ValueTypeTcp) || (portType == ValueTypeHttp && port.Type == ValueTypeUdp) || (portType == ValueTypeHttps && port.Type == ValueTypeUdp) {
			slog.Error("Cannot create mapping for port as it is currently defined! Inconsistency in defined port and docker port identity, defaulting to docker identity!", "defined", portType, "docker", port.Type, "port", port.ContainerPort)
			portType = port.Type
		}

		useSsl := StringOrElse(GetLabelForPort(labels, LabelGlobalSsl, LabelPortSsl, port.ContainerPort), "true") == "true"
		bindAddress := StringOrElse(GetLabelForPort(labels, LabelGlobalBind, LabelPortBind, port.ContainerPort), "0.0.0.0")
		domain := StringOrElse(GetLabelForPort(labels, LabelGlobalDomain, LabelPortDomain, port.ContainerPort), config.DefaultDomain)

		if portType == ValueTypeHttp || portType == ValueTypeHttps {
			nonstandardPort := StringOrElse(GetLabelForPort(labels, LabelGlobalNonstandardHttp, LabelPortNonstandardHttp, port.ContainerPort), "false") == "true"

			var portString string
			if nonstandardPort {
				portString = StringOrElse(GetLabelForPort(labels, LabelGlobalPortOverride, LabelPortPortOverride, port.ContainerPort), strconv.Itoa(int(port.ContainerPort)))
			} else if useSsl {
				portString = "443"
			} else {
				portString = "80"
			}

			slog.Debug("Port configuration", "type", portType, "ssl", useSsl, "bind", bindAddress, "domain", domain, "nonstandard", nonstandardPort, "port", portString, "port", port.ContainerPort, "binding", port.Binding)

			http += NginxHttp(
				bindAddress+":"+portString,
				useSsl,
				domain,
				portType,
				port.Binding,
				port.HostPort,
				config,
			)
		} else if portType == ValueTypeTcp {
			slog.Debug("Port configuration", "type", portType, "ssl", useSsl, "bind", bindAddress, "domain", domain, "port", port.ContainerPort, "binding", port.Binding)

			service += NginxTcp(
				bindAddress+":"+strconv.Itoa(int(port.ContainerPort)),
				useSsl,
				port.Binding,
				port.HostPort,
				config,
			)
		} else if portType == ValueTypeUdp {
			slog.Debug("Port configuration", "type", portType, "ssl", useSsl, "bind", bindAddress, "domain", domain, "port", port.ContainerPort, "binding", port.Binding)

			service += NginxUdp(
				bindAddress+":"+strconv.Itoa(int(port.ContainerPort)),
				port.Binding,
				port.HostPort,
			)
		} else {
			slog.Warn("Failed to create binding because the protocol was not recognised", "protocol", portType)
		}
	}

	return NginxProjectBinding{
		HttpContent:    http,
		ServiceContent: service,
	}, nil
}

// GenerateFilesForProjectNginxBinding will iterate through the list of containers in the provided project, and for each
// inspect them, and generate the corresponding nginx configs using GenerateFilesForContainerNginxBinding. The resulting
// configs are merged and returned as a new single config instance
func GenerateFilesForProjectNginxBinding(cli *client.Client, ctx context.Context, project BindingProject, config BindingConfiguration) (*NginxProjectBinding, error) {
	result := NginxProjectBinding{
		HttpContent:    "",
		ServiceContent: "",
	}

	for _, container := range project.Containers {
		cnt, err := cli.ContainerInspect(ctx, container.Name)
		if err != nil {
			slog.Error("Failed to create bindings for container due to an error retrieving container from docker host", "project", project.Project, "container", container.Name, "error", err)
			return nil, err
		}

		binding, err := GenerateFilesForContainerNginxBinding(
			container,
			cnt.Config.Labels,
			config,
		)
		if err != nil {
			slog.Error("Failed to create bindings for container due to an error", "project", project.Project, "container", container.Name, "error", err)
			return nil, err
		}

		result.HttpContent += binding.HttpContent
		result.ServiceContent += binding.ServiceContent
	}

	return &result, nil
}

// GenerateFilesForNginxBinding will iterate through the provided set of projects and for each generate the required
// nginx configurations for routing traffic. Each config file will be generated in the format `<project>.svc.http.conf`
// for http traffic  and `<project>.svc.plain.conf` for UDP/TCP traffic. The result is compatible with
// WriteFileSetWithDiff
func GenerateFilesForNginxBinding(cli *client.Client, ctx context.Context, binding BindingResult, config BindingConfiguration) (map[string]string, error) {
	result := make(map[string]string)

	for _, project := range binding.Projects {
		httpFilename := project.Project + ".svc.http.conf"
		serviceFilename := project.Project + ".svc.plain.conf"

		nginxBinding, err := GenerateFilesForProjectNginxBinding(cli, ctx, project, config)
		if err != nil {
			slog.Error("Failed to generate files for nginx binding", "project", project.Project, "error", err)
			return nil, err
		}

		if len(nginxBinding.HttpContent) > 0 {
			result[httpFilename] = nginxBinding.HttpContent
		}
		if len(nginxBinding.ServiceContent) > 0 {
			result[serviceFilename] = nginxBinding.ServiceContent
		}
	}

	return result, nil
}
