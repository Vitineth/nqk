package internal

import (
	"github.com/kylelemons/godebug/diff"
	"log/slog"
	"os"
	"path/filepath"
)

// WriteFileSetWithDiff will write the provided set of files as long as the contents are different. For any file for which
// the content is the same, the file will be skipped. The method will return whether any files were written at all out
// of the provided set.
//
// The keys in the files map will correspond to the file names, which are joined with the given prefix using
// filepath.Join. If an error is encountered while reading the file, the file will be written, any errors encountered
// during the process will be returned immediately meaning not all writes will be attempted, however the boolean status
// will always be accurate.
func WriteFileSetWithDiff(files map[string]string, filePrefix string) (bool, error) {
	hasWritten := false
	for key, value := range files {
		path := filepath.Join(filePrefix, key)
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Debug("Writing file because there was an error reading", "file", path, "error", err)
			err := os.WriteFile(path, []byte(value), 0666)
			if err != nil {
				slog.Error("Failed to write file with nginx binding", "file", path, "error", err)
				return hasWritten, err
			}
			hasWritten = true
			continue
		}

		if string(data) == value {
			slog.Debug("Skipping file because contents are the same", "file", path)
			continue
		}

		slog.Debug("Writing file because content is different", "file", path, "diff", diff.Diff(string(data), value))

		err = os.WriteFile(path, []byte(value), 0666)
		if err != nil {
			slog.Error("Failed to write file with nginx binding", "file", path, "error", err)
			return hasWritten, err
		}
		hasWritten = true
	}

	return hasWritten, nil
}
