package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/supervisor"
)

func main() {
	configPath := flag.String("config", "config.json", "configuration file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if home, err := os.UserHomeDir(); err == nil {
		if err := config.LoadDotEnv(filepath.Join(home, ".env")); err != nil {
			fatal(err)
		}
	}
	if err := config.LoadDotEnv(".env"); err != nil {
		fatal(err)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err)
	}

	manager := supervisor.NewManager(cfg.Supervisord, logger)
	manager.Start(ctx)
	server := supervisor.Server{Addr: cfg.Supervisord.Addr, Manager: manager}
	fmt.Fprintf(os.Stdout, "supervisord listening on %s\n", cfg.Supervisord.Addr)
	if err := server.Listen(ctx); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	if err == nil {
		return
	}
	slog.Error("supervisord fatal", "error", err)
	fmt.Fprintln(os.Stderr, "supervisord:", err)
	os.Exit(1)
}
