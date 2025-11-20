package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/phsym/console-slog"
)

func main() {
	ctx := context.Background()
	config := parseFlags()

	initLogger(config.LogType)

	if config.ServeUI {
		ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)

		if err := serveDashboard(ctx, config); err != nil {
			slog.ErrorContext(ctx, "UI server failed", slogErr(err))
			cancel()
			os.Exit(1)
		}
		cancel()
		return
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "Starting", slog.Any("config", fmt.Sprintf("%#v", config)))
	if err := run(ctx, config); err != nil {
		slog.ErrorContext(ctx, "Benchmark failed", slogErr(err))
		os.Exit(1)
	}
}

func initLogger(logType LogType) {
	var handler slog.Handler
	switch logType {
	case LogVerbose:
		handler = console.NewHandler(os.Stderr, &console.HandlerOptions{
			Level: slog.LevelDebug,
		})
	case LogDisabled:
		handler = slog.DiscardHandler
	default:
		handler = console.NewHandler(os.Stderr, &console.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
}
