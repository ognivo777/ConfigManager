package cli

import (
	"log/slog"
	"os"

	"github.com/example/cm/internal/api"
	"github.com/example/cm/internal/config"
	"github.com/example/cm/internal/daemon"
)

// runDaemon starts the daemon, the API server, and blocks until interrupted.
func runDaemon(root string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	d, err := daemon.New(root, logger)
	if err != nil {
		return err
	}
	defer d.Close()

	if err := d.Start(); err != nil {
		return err
	}

	server := api.NewServer(config.SocketPath(root), api.NewHandler(d), logger)
	if err := server.Start(); err != nil {
		return err
	}
	defer server.Close()

	logger.Info("cm daemon started", "root", root, "socket", config.SocketPath(root))

	// block until interrupted
	wait := make(chan struct{})
	<-wait
	return nil
}
