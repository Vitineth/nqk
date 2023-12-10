package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/mod/semver"
	"log/slog"
	"nqk/internal"
	"os"
	"os/user"
	"strings"
)

const SystemdApplyServiceFile = "/etc/systemd/system/nqkd-apply.service"
const ApplySystemdTemplate = `[Unit]
Description=nqkd - apply
After=network.target

[Service]
ExecStart=nqkd launch {{path}} 
Restart=on-failure
RuntimeDirectory=nqkd
RuntimeDirectoryMode=0777

[Install]
WantedBy=multi-user.target`

const NginxSystemdTemplate = `[Unit]
Description=nqkd nginx
After=network.target

[Service]
ExecStart=nqkd --path /home/ryan/system-atlas/nqkd/ --watch binding nginx --ssl-cert /var/secrets/_.erebus.xiomi-aion.co.uk.crt --ssl-privkey /var/secrets/_.erebus.xiomi-aion.co.uk.key --domain erebus.xiomi-aion.co.uk --dir /etc/nginx/conf.d/nqkd/ --executable /usr/sbin/nginx --service=nginx
Restart=on-failure

[Install]
WantedBy=multi-user.target`

const InstallLocation = "/usr/bin/nqkd"
const Check = "\u2705 "
const Cross = "\u274C "
const Pending = "\u23F3 "

func Install(paths []string) {
	ensureSudo()
	tryStopServices()
	checkInstalledVersion()
	checkForDockerInstall()
	checkDockerComposeInstall()
	checkDockerFunctionality()
	checkSystemdFiles(paths)
}

func ensureSudo() {
	slog.Info(Pending + "Checking if current user is usable")

	currentUser, err := user.Current()
	if err != nil {
		slog.Error(Cross+"Failed to query the current user!", "error", err)
		os.Exit(1)
	}

	if currentUser.Username == "root" {
		slog.Info(Check + "User is root")
		return
	}

	slog.Error(Cross + "Current user cannot write to the install location")
	os.Exit(1)
}

func tryStopServices() {
	slog.Info(Pending + "Trying to stop any running services")
	err := internal.Run("systemctl", "stop", "nqkd-apply").Run()
	if err != nil {
		slog.Warn(Cross+"Failed to stop nqkd-apply but we don't really care", "error", err)
	} else {
		slog.Info(Check + "Stopped nqkd-apply")
	}

	err = internal.Run("systemctl", "stop", "nqkd-nginx").Run()
	if err != nil {
		slog.Warn(Cross+"Failed to stop nqkd-nginx but we don't really care", "error", err)
	} else {
		slog.Info(Check + "Stopped nqkd-nginx")
	}
}

func checkInstalledVersion() {
	slog.Info(Pending + "Checking currently installed version of nqkd")
	output, err := internal.Run(InstallLocation, "version").CombinedOutput()
	if err != nil {
		slog.Info(Pending+"Failed to run nqkd, copying this executable to "+InstallLocation, "error", err)

		installFile()
	} else {
		result := semver.Compare(Version, string(output[:]))
		if result == 0 {
			slog.Info(Check+"Found version matching this one already installed", "this", Version, "query", string(output[:]))
		} else if result < 0 {
			slog.Error(Cross + "Found a version newer than us! Exiting as this will cause a downgrade. To continue, delete the executable and re-run")
			os.Exit(1)
		} else {
			slog.Info(Pending + "Found a version older than us, trying to upgrade")
			installFile()
		}
	}
}

func installFile() {
	executable, err := os.Executable()
	if err != nil {
		slog.Error(Cross + "Could not determine current executable")
		os.Exit(1)
	}

	file, err := os.ReadFile(executable)
	if err != nil {
		slog.Error(Cross + "Could not read current executable")
		os.Exit(1)
	}

	err = os.WriteFile(InstallLocation, file, 0644)
	if err != nil {
		slog.Error(Cross+"Could not write executable to the install location", "target", InstallLocation, "error", err)
		os.Exit(1)
	}

	slog.Info(Check+"Installed to location", "target", InstallLocation)
}

func checkForDockerInstall() {
	output, err := internal.Run("docker", "version").CombinedOutput()
	if err != nil {
		slog.Error(Cross + "Could not run 'docker' - cannot continue without a docker runtime")
		os.Exit(1)
	}

	if !strings.HasPrefix(string(output[:]), "Client") {
		slog.Error(Cross+"Found unexpected start of the docker version", "wanted", "Client:", "got", string(output[:])[0:15])
		os.Exit(1)
	}

	slog.Info(Check + "Found a docker command we can execute")
}

func checkDockerComposeInstall() {
	output, err := internal.Run("docker", "compose", "version").CombinedOutput()
	if err != nil {
		slog.Error(Cross + "Could not run 'docker compose' - cannot continue without a docker compose install")
		os.Exit(1)
	}

	if !strings.HasPrefix(string(output[:]), "Docker Compose version") {
		slog.Error(Cross+"Found unexpected start of the docker version", "wanted", "Docker Compose version", "got", string(output[:])[0:30])
		os.Exit(1)
	}

	if semver.Compare(strings.TrimSpace(string(output[:]))[23:], "v2.18.0") < 0 {
		slog.Error(Cross + "Docker compose version is too old, this version does not support --dry-run which is required for checking files")
		os.Exit(1)
	}

	slog.Info(Check + "Found a docker compose command we can execute")
}

func checkDockerFunctionality() {
	output, err := internal.Run("docker", "container", "ls", "--format", "{{json .}}").CombinedOutput()
	if err != nil {
		slog.Error(Cross + "Could not run 'docker compose' - cannot continue without a docker compose install")
		os.Exit(1)
	}

	lines := strings.Split(strings.TrimSpace(string(output[:])), "\n")
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			slog.Error(Cross+"Got invalid output from docker, failed parsing line", "line", line)
			os.Exit(1)
		}
	}

	slog.Info(Check + "Docker is functional!")
}

func checkSystemdFiles(paths []string) {
	file, err := os.ReadFile(SystemdApplyServiceFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Info(Pending + "Systemd file does not exist, creating")
			createSystemdApplyFile(paths)
			return
		} else {
			slog.Error(Cross+"Cannot find systemd file due to an error", "error", err)
			os.Exit(1)
		}
	}

	if strings.TrimSpace(generateSystemdFile(paths)) == string(file[:]) {
		slog.Info(Check + "Service file is present and up to date")
		startService()
	} else {
		slog.Info(Pending + "Service file is out of date, do you want to overwrite it? [Y/n]")
		var in string
		_, err := fmt.Scanln(&in)
		if err != nil {
			slog.Error(Cross + "Could not read from stdin")
			os.Exit(1)
		}

		if strings.ToLower(in) == "y" {
			slog.Info(Pending + "Trying to overwrite")
			createSystemdApplyFile(paths)
		} else {
			slog.Info(Cross + "Aborting")
		}
	}
}

func generateSystemdFile(paths []string) string {
	pathSegment := ""

	for i := 0; i < len(paths); i++ {
		pathSegment += " --path \"" + paths[i] + "\""
	}
	if len(pathSegment) > 0 {
		pathSegment = pathSegment[1:]
	}

	return strings.ReplaceAll(ApplySystemdTemplate, "{{path}}", pathSegment)
}

func createSystemdApplyFile(paths []string) {
	pathSegment := ""
	for i := 0; i < len(paths); i++ {
		pathSegment += " --path \"" + paths[i] + "\""
	}

	err := os.WriteFile(SystemdApplyServiceFile, []byte(generateSystemdFile(paths)), 0644)
	if err != nil {
		slog.Error("Failed to write systemd file!", "error", err)
		os.Exit(1)
	}

	slog.Info(Check + "Wrote systemd file")

	startService()
}

func startService() {
	slog.Info(Pending + "Trying to restart service")
	err := internal.Run("systemctl", "daemon-reload").Run()
	if err != nil {
		slog.Error("Couldn't reload the systemctl daemon", "error", err)
		os.Exit(1)
	}

	err = internal.Run("systemctl", "restart", "nqkd-apply").Run()
	if err != nil {
		slog.Info(Cross+"Failed to restart service", "error", err)
		os.Exit(1)
	}

	slog.Info(Check + "Restarted service")
}
