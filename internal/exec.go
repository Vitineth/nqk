package internal

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// Run is a wrapper around exec.Command which logs the requested command and arguments, and also copies the active
// environment into the subprocess. This is designed to work around some issues however may pose a security concern.
func Run(command string, arg ...string) *exec.Cmd {
	slog.Debug("run :: " + command + " " + strings.Join(arg, " "))
	cmd := exec.Command(command, arg...)
	cmd.Env = os.Environ()
	return cmd
}
