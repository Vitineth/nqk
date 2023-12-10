package internal

import (
	"bytes"
	"log/slog"
)

// ValidateNginx will execute the provided nginx executable with the `-t` flag which should validate the config
// currently written to disk. In the event the command fails, it will return false with the error returned from the
// cmd.Run() command.
func ValidateNginx(executable string) (bool, error) {
	var validationOutputBuffer bytes.Buffer
	validateCmd := Run(executable, "-t")
	validateCmd.Stdout = &validationOutputBuffer

	err := validateCmd.Run()
	if err != nil {
		slog.Error("Failed to validate the config due to an error. This means the config may not have validated", "error", err, "stdout", validationOutputBuffer.String())
		return false, err
	}

	return true, nil
}

// RelaunchNginx will attempt to call out to /usr/sbin/service to restart the provided service name (this will usually
// just be nginx but in case it varies, this is supported). In the event the command fails, the error from the cmd.Run()
// will be returned. It is recommended to call ValidateNginx first to ensure that we don't try and relaunch nginx into
// an invalid state if it is already running successfully
func RelaunchNginx(service string) error {
	var restartOutputBuffer bytes.Buffer
	restartCmd := Run("/usr/sbin/service", service, "restart")
	restartCmd.Stdout = &restartOutputBuffer

	err := restartCmd.Run()
	if err != nil {
		slog.Error("Failed to restart nginx due to an error!", "error", err, "stdout", restartOutputBuffer.String())
		return err
	}

	return nil
}
