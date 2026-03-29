package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"ton-monitoring/internal/app"
	"ton-monitoring/internal/config"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	version = "0.1.0"
	commit  = "none"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}

	debug.SetMemoryLimit(cfg.MemLimit)
	debug.SetGCPercent(-1)

	level, _ := zerolog.ParseLevel(cfg.LogLevel)
	zerolog.SetGlobalLevel(level)
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		<-sigCh
		log.Warn().Msg("forced exit")
		os.Exit(1)
	}()

	log.Info().
		Str("version", version).
		Str("commit", commit).
		Int("addresses", len(cfg.WatchAddresses)).
		Str("health", cfg.HealthAddr).
		Msg("starting ton-monitor")

	application, err := app.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize application")
	}

	if err := application.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error().Err(err).Msg("application error")
	}

	log.Info().Msg("shutdown initiated")
	application.Shutdown()
}
