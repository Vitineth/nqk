package internal

import (
	"gopkg.in/fsnotify/fsnotify.v1"
	"io/fs"
	"log/slog"
	"path/filepath"
	"time"
)

// WatchAndExecute will create a new fsnotify watcher on the provided set of paths. On any change to the files, it will
// call the executor. If an every value is supplied, this will also launch a goroutine to call the executor every period.
// This method call is blocking as it waits for file system events so should likely be launched in its own goroutine
// if you need to perform other actions at the same time
func WatchAndExecute(paths []string, executor func(), every *time.Duration) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("Failed to launch the watching system due to an error", err)
		return err
	}

	for _, path := range paths {
		err := watcher.Add(path)
		if err != nil {
			slog.Error("Failed to add path to the watch system, exiting early - trying to close", "error", err)
			err = watcher.Close()
			if err != nil {
				slog.Error("Couldn't close the watcher!", "error", err)
				return err
			}

			return err
		}
	}

	defer func(watcher *fsnotify.Watcher) {
		err := watcher.Close()
		if err != nil {
			slog.Error("Failed to close watcher on exit")
		}
	}(watcher)

	if every != nil {
		go func() {
			for {
				slog.Info("Triggering executor due to time schedule")
				executor()
				time.Sleep(*every)
			}
		}()
	}

	for event := range watcher.Events {
		slog.Info("Received an event, launching executor", "event", event)
		executor()
	}

	return nil
}

// LoadProjectsFromPaths will walk every provided path and locate any specified .yaml files, attempting to process them
// with ProcessDockerComposeFile. Any projects that fail will be skipped and the errors will be logged but not returned.
// Errors will only be returned if there is a problem walking the file tree itself.
func LoadProjectsFromPaths(paths []string) ([]ProcessedDockerComposeFile, error) {
	files := make([]string, 0)
	for _, path := range paths {
		err := filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			if filepath.Ext(d.Name()) == ".yaml" {
				files = append(files, path)
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	projects := make([]ProcessedDockerComposeFile, 0)
	for _, file := range files {
		updated, err := ProcessDockerComposeFile(file)
		if err != nil {
			slog.Error("Failed to load configuration file due to error", "file", file, "error", err)
			continue
		}

		projects = append(projects, *updated)
	}

	return projects, nil
}
