package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/client"
	"log/slog"
	"nqk/internal"
	"os"
	"sync"
	"time"
)

func GenerateBindings(ctx *globalContext, b *BindingStruct) (*client.Client, *context.Context, *internal.BindingResult, error) {
	projects, err := internal.LoadProjectsFromPaths(b.Paths)
	if err != nil {
		slog.Error("Failed to load set of projects due to error", err)
		os.Exit(1)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.Error("Failed to create the docker client!", "error", err)
		os.Exit(1)
	}
	defer func(cli *client.Client) {
		err := cli.Close()
		if err != nil {
			slog.Error("Failed to close the CLI client due to error!", "error", err)
		}
	}(cli)

	dctx := context.Background()
	result, err := internal.GetBindingsForAllProjects(cli, dctx, projects)
	if err != nil {
		slog.Error("Failed to get bindings for all projects due to an error", "error", err)
		return cli, &dctx, nil, err
	}

	return cli, &dctx, &result, nil
}

func RunNginxBinding(n *NginxStruct, b *BindingStruct, ctx *globalContext) error {
	cli, dctx, bindings, err := GenerateBindings(ctx, b)
	if err != nil {
		return err
	}

	binding, err := internal.GenerateFilesForNginxBinding(
		cli,
		*dctx,
		*bindings,
		internal.BindingConfiguration{
			DefaultDomain:  CLI.Binding.DefaultDomain,
			SslCertificate: CLI.Binding.SslCertificate,
			SslPrivateKey:  CLI.Binding.SslPrivateKey,
		},
	)
	if err != nil {
		slog.Error("Failed to generate nginx configuration due to error", "error", err)
		return err
	}

	needsUpdate, err := internal.WriteFileSetWithDiff(binding, n.OutDir)
	if err != nil {
		if needsUpdate {
			slog.Error("Failed to write all files due to an error, however some files were edited", "error", err)
		} else {
			slog.Error("Failed to write all files due to an error, no files were changed", "error", err)
		}
		return nil
	}

	if needsUpdate {
		slog.Info("Files written to target, system now needs updating")

		if n.Executable == nil {
			slog.Info("Can't handle automatic restarts because no executable has been provided")
		} else {
			if ok, err := internal.ValidateNginx(*n.Executable); !ok || err != nil {
				slog.Error("Failed to restart nginx because the generated config is invalid!", "ok", ok, "error", err)
			} else {
				err := internal.RelaunchNginx(n.ServiceName)
				if err != nil {
					slog.Error("Failed to restart nginx - the config was valid but failed to launch", "error", err)
					return err
				}
			}
		}
	} else {
		slog.Info("No changes made")
	}

	return nil
}

func RunJsonBinding(ctx *globalContext, b *BindingStruct) error {
	_, _, bindings, err := GenerateBindings(ctx, b)
	if err != nil {
		return err
	}

	marshal, err := json.Marshal(bindings)
	if err != nil {
		slog.Error("Successfully queried for bindings but failed to marshall to json", "error", err)
		return err
	}

	fmt.Printf("%v", string(marshal))
	return nil
}

func RunJson(b *BindingStruct, ctx *globalContext) error {
	if b.Watch {
		oneMinute := 1 * time.Minute
		err := internal.WatchAndExecute(
			b.Paths,
			func() {
				err := RunJsonBinding(ctx, b)
				if err != nil {
					slog.Error("Failed to execute json bindings due to error!", "error", err)
				}
			},
			&oneMinute,
		)
		if err != nil {
			slog.Error("Failed to launch the watcher", "error", err)
			return err
		}
	} else {
		err := RunJsonBinding(ctx, b)
		if err != nil {
			return err
		}
	}

	return nil
}

func RunNginx(n *NginxStruct, b *BindingStruct, ctx *globalContext) error {
	if b.Watch {
		var lock sync.Mutex
		events := make(chan internal.DockerEvent, 30)

		executor := func() {
			lock.Lock()
			err := RunNginxBinding(n, b, ctx)
			if err != nil {
				slog.Error("Failed to execute nginx bindings due to error!", "error", err)
			}
			lock.Unlock()
		}

		go func() {
			for event := range events {
				switch event.Type.TypeMeta {
				case internal.ContainerDestroyEvent.TypeMeta:
				case internal.ContainerDetachEvent.TypeMeta:
				case internal.ContainerDieEvent.TypeMeta:
				case internal.ContainerKillEvent.TypeMeta:
				case internal.ContainerRestartEvent.TypeMeta:
				case internal.ContainerOomEvent.TypeMeta:
				case internal.ContainerStartEvent.TypeMeta:
				case internal.ContainerStopEvent.TypeMeta:
					slog.Info("Got event to trigger rebind", "event", event)
					executor()
				default:
					slog.Debug("Got unsupported event dropping", "event", event)
				}
			}
		}()

		err := internal.SubscribeToDockerEvents(events)
		if err != nil {
			slog.Error("Could not start watching, could not attach to docker events", "error", err)
			return err
		}

		oneMinute := 1 * time.Minute
		err = internal.WatchAndExecute(
			b.Paths,
			executor,
			&oneMinute,
		)
		if err != nil {
			slog.Error("Failed to launch the watcher", "error", err)
			return err
		}
	} else {
		err := RunNginxBinding(n, b, ctx)
		if err != nil {
			return err
		}
	}

	return nil
}
