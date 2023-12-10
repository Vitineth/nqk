package main

import (
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"github.com/rodaine/table"
	"log/slog"
	"net/rpc"
	"nqk/internal"
	"nqk/internal/nrpc"
	"os"
	"slices"
	"strings"
)

func makeRpc(cli *InnerCli) (*rpc.Client, error) {
	return rpc.DialHTTP("unix", cli.SocketFile)
}

func Apply(cli *InnerCli) {
	client, err := makeRpc(cli)
	if err != nil {
		slog.Error("Failed to initialise the connection with the daemon due to an error!", "error", err)
		os.Exit(1)
	}

	err = nrpc.ForceApply(client)
	if err != nil {
		slog.Error("Failed to request an apply from the daemon", "error", err)
		os.Exit(1)
	}

	slog.Info("Request submitted successfully")
}

func Status(cli *InnerCli, options *InnerStatus) {
	client, err := makeRpc(cli)
	if err != nil {
		slog.Error("Failed to initialise the connection with the daemon due to an error!", "error", err)
		os.Exit(1)
	}

	status, err := nrpc.GetAllStatus(client)
	if err != nil {
		slog.Error("Failed to query for the current status", "error", err)
		os.Exit(1)
	}
	slices.SortFunc(status, func(a, b internal.ActiveProjectState) int {
		return strings.Compare(a.Project.Name, b.Project.Name)
	})

	if options.Format == "json" {
		marshal, err := json.Marshal(status)
		if err != nil {
			slog.Error("Received the status from the daemon, but it failed to serialise to JSON", "error", err)
			os.Exit(1)
		}

		fmt.Printf("%v", marshal)
	} else if options.Format == "table" {
		headerFmt := color.New(color.FgGreen, color.Underline).SprintfFunc()
		columnFmt := color.New(color.FgYellow).SprintfFunc()
		tbl := table.New("Project", "Source", "Last Updated", "Current State")
		tbl.WithHeaderFormatter(headerFmt).WithFirstColumnFormatter(columnFmt)
		for _, v := range status {
			tbl.AddRow(v.Project.Name, v.Project.Source, v.LastUpdated, stateToString(v.State))
		}
		tbl.Print()

		if len(status) == 0 {
			println("(no records)")
		}
	} else {
		slog.Error("Unknown data format - one of json and table are required")
	}
}

func stateToString(state internal.ProjectState) string {
	switch state {
	case internal.ProjectApplying:
		return "Applying"
	case internal.ProjectOk:
		return "Ok"
	case internal.ProjectFailed:
		return "Failed"
	case internal.ProjectMissing:
		return "Missing"
	case internal.ProjectSeen:
		return "Seen"
	default:
		return "Unknown (!!)"
	}
}
