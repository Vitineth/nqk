package main

import (
	"log/slog"
	"nqk/internal"
	"nqk/internal/nrpc"
	"os"
	"sync"
	"time"
)

func runLaunchCommand(l *LaunchStruct, record *internal.StateRecord) error {
	slog.Info("Checking all projects...")
	projects, err := internal.LoadProjectsFromPaths(l.Paths)
	if err != nil {
		slog.Error("Failed to load set of projects due to error", err)
		os.Exit(1)
	}

	for _, v := range projects {
		if _, ok := record.Projects[v.Name]; ok {
			record.Update(v, internal.ProjectSeen)
		} else {
			record.Update(v, internal.ProjectSeen)
		}
	}
	for k, v := range record.Projects {
		if v.State != internal.ProjectSeen {
			record.UpdateByName(k, internal.ProjectMissing)
		}
	}

	for _, project := range projects {
		needsApplying, err := internal.DoesProjectNeedApplying(project)
		if err != nil {
			slog.Error("Could not tell if the project needs applying - ran into an error running the command", "file", project.Source, "error", err)
			continue
		}

		if needsApplying {
			record.Update(project, internal.ProjectApplying)
			slog.Info("File needs applying", "file", project.Source)
			if l.DryRun {
				slog.Info("Not applying changes because this is a dry run!")
				record.Update(project, internal.ProjectOk)
			} else {
				if err = internal.ApplyCompose(project); err != nil {
					record.Update(project, internal.ProjectFailed)
					slog.Error("Failed to apply update due to an error", "error", err, "file", project.Source)
				} else {
					record.Update(project, internal.ProjectOk)
				}
			}
		} else {
			slog.Debug("File does not need applying", "file", project.Source)
			record.Update(project, internal.ProjectOk)
		}
	}
	return nil
}

func Launch(l *LaunchStruct) error {
	var lock sync.Mutex
	action := make(chan internal.DaemonCommand, 10)

	record := internal.StateRecord{
		Projects: map[string]*internal.ActiveProjectState{},
		//Lock:     &lock,
	}

	executor := func() {
		lock.Lock()
		err := runLaunchCommand(l, &record)
		if err != nil {
			slog.Error("Failed to execute launch due to error!", "error", err)
		}
		lock.Unlock()
	}

	go func() {
		for command := range action {
			switch command {
			case internal.CommandApply:
				executor()
			}
		}
	}()

	go func() {
		err := nrpc.Launch(record, action)
		if err != nil {
			slog.Error("Failed to launch the rpc server", "error", err)
		}
	}()

	//if l.Watch {
	fiveMinutes := 5 * time.Minute
	err := internal.WatchAndExecute(
		l.Paths,
		executor,
		&fiveMinutes,
	)
	if err != nil {
		slog.Error("Failed to launch the watcher", "error", err)
		return err
	}
	//} else {
	//	err := runLaunchCommand(l, ctx)
	//	if err != nil {
	//		return err
	//	}
	//}

	return nil
}
