// Package service wires cm into the operating-system service manager using
// github.com/kardianos/service.
package service

import (
	"log/slog"
	"os"

	"github.com/example/cm/internal/api"
	"github.com/example/cm/internal/config"
	"github.com/example/cm/internal/daemon"
	"github.com/kardianos/service"
)

// Program implements kardianos/service.Program and runs the cm daemon in the
// service context.
type Program struct {
	root string
	log  *slog.Logger
	done chan struct{}
	api  *api.Server
}

// NewProgram returns a Program configured for the given repository root.
func NewProgram(root string) *Program {
	if root == "" {
		root = config.DefaultRepository
	}
	return &Program{
		root: root,
		log:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
}

// Start launches the daemon and the API server.
func (p *Program) Start(s service.Service) error {
	p.done = make(chan struct{})

	d, err := daemon.New(p.root, p.log)
	if err != nil {
		return err
	}
	if err := d.Start(); err != nil {
		return err
	}

	srv := api.NewServer(config.SocketPath(p.root), api.NewHandler(d), p.log)
	if err := srv.Start(); err != nil {
		return err
	}
	p.api = srv
	p.log.Info("cm service running as "+s.String()+" daemon", "root", p.root)
	return nil
}

// Stop shuts the daemon and the API server down.
func (p *Program) Stop(s service.Service) error {
	close(p.done)
	if p.api != nil {
		_ = p.api.Close()
	}
	return nil
}

// Config describes the service. Install passes root so the OS service unit
// retains the correct repository path.
func Config(root string) *service.Config {
	if root == "" {
		root = config.DefaultRepository
	}
	exe, _ := os.Executable()
	return &service.Config{
		Name:        "cm",
		DisplayName: "cm Config Manager",
		Description: "Monitors configuration files and maintains Git-backed history.",
		Arguments:   []string{"-d", root},
		Executable:  exe,
	}
}

// Install installs the operating-system service for the given repository root.
func Install(root string) error {
	if root == "" {
		root = config.DefaultRepository
	}
	cfg := Config(root)
	svc, err := service.New(NewProgram(root), cfg)
	if err != nil {
		return err
	}
	return svc.Install()
}

// Uninstall removes the operating-system service.
func Uninstall() error {
	cfg := Config(config.DefaultRepository)
	svc, err := service.New(NewProgram(config.DefaultRepository), cfg)
	if err != nil {
		return err
	}
	return svc.Uninstall()
}

// Control performs a service control action (start/stop/restart).
func Control(action string) error {
	cfg := Config(config.DefaultRepository)
	svc, err := service.New(NewProgram(config.DefaultRepository), cfg)
	if err != nil {
		return err
	}
	return service.Control(svc, action)
}
