// Command weir is a window manager for the river Wayland compositor.
//
// weir must be started by river (or another compositor implementing
// river-window-management-v1), typically from the river init script:
//
//	exec weir &
//
// It connects to the Wayland display named by the environment, takes the
// window manager role, and manages windows until the compositor exits.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/psanford/weir/bridge"
	"github.com/psanford/weir/core"
	"github.com/psanford/weir/wire"
)

var version = "0.1.0-dev"

func main() {
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("weir", version)
		return
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		fmt.Fprintf(os.Stderr, "weir: invalid log level %q\n", *logLevel)
		os.Exit(2)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	conn, err := wire.Connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	model := core.NewModel()
	b := bridge.New(conn, model, logger)
	if err := b.Bootstrap(); err != nil {
		if errors.Is(err, bridge.ErrUnavailable) {
			return err
		}
		return fmt.Errorf("bootstrap: %w", err)
	}
	logger.Info("weir started", "version", version)

	// The command channel is wired up to the IPC socket in a future
	// milestone; until then the bridge is driven entirely by the
	// compositor.
	cmds := make(chan bridge.Command)
	return b.Run(cmds)
}
