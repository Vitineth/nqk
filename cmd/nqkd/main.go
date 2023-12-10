package main

import (
	"fmt"
	"github.com/alecthomas/kong"
	"log/slog"
	"os"
)

const Version = "v0.0.7"

type globalContext struct {
}

// Daemon

type LaunchStruct struct {
	Paths  []string `help:"The set of folders to watch for changes and query for updates" name:"path" type:"path"`
	DryRun bool     `help:"Don't actually apply any changes, just list what files need applying'" name:"dry-run"`
}

func (l *LaunchStruct) Run(ctx *globalContext) error {
	return Launch(l)
}

// Bindings

type BindingStruct struct {
	Paths []string `help:"The set of folders to watch for changes and query for updates" name:"path" type:"path"`
	Watch bool     `name:"watch" default:"false"`

	SslCertificate string      `name:"ssl-cert"`
	SslPrivateKey  string      `name:"ssl-privkey"`
	DefaultDomain  string      `name:"domain"`
	Nginx          NginxStruct `cmd:""`
	Json           JsonStruct  `cmd:""`
}

type NginxStruct struct {
	OutDir      string  `name:"dir" default:"."`
	Executable  *string `name:"executable"`
	ServiceName string  `name:"service" default:"nginx"`
}

func (n *NginxStruct) Run(ctx *globalContext, b *BindingStruct) error {
	return RunNginx(n, b, ctx)
}

type JsonStruct struct {
}

func (j *JsonStruct) Run(ctx *globalContext, b *BindingStruct) error {
	return RunJson(b, ctx)
}

// Inner CLI

type InnerCli struct {
	SocketFile string      `name:"socket" default:"/run/nqkd/nqkd.sock"`
	Apply      InnerApply  `cmd:""`
	Status     InnerStatus `cmd:""`
}

type InnerApply struct {
}

func (a *InnerApply) Run(cli *InnerCli) error {
	Apply(cli)
	return nil
}

type InnerStatus struct {
	Format string `name:"format" enum:"json,table" default:"table"`
}

func (s *InnerStatus) Run(cli *InnerCli) error {
	Status(cli, s)
	return nil
}

// Version

type VersionCommand struct{}

func (v *VersionCommand) Run() error {
	fmt.Printf("%v\n", Version)
	return nil
}

// Install

type InstallCommand struct {
	Paths []string `help:"The set of folders to watch for changes and query for updates" name:"path" type:"path"`
}

func (i *InstallCommand) Run() error {
	Install(i.Paths)
	return nil
}

// Final CLI

var CLI struct {
	Launch  LaunchStruct   `cmd:"" help:"Launch the nqk daemon to start applying configurations from the path"`
	Binding BindingStruct  `cmd:"" help:"List bindings of current deployments"`
	Cli     InnerCli       `cmd:"" help:"Interact with the running daemon"`
	Install InstallCommand `cmd:"" help:"Checks the installation of this daemon and installs if its missing"`
	Version VersionCommand `cmd:"" help:"The current version"`
}

func main() {
	level := new(slog.LevelVar)
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
	level.Set(slog.LevelDebug)

	ctx := kong.Parse(&CLI, kong.Name("nqkd"),
		kong.Description("Not Quite Kubernetes Daemon"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
			Summary: true,
		}))
	err := ctx.Run(&globalContext{})
	ctx.FatalIfErrorf(err)
}
