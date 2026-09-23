package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/handsomefox/dnsbench/internal/web"
	"github.com/phsym/console-slog"
)

func main() {
	ctx := context.Background()
	useAndroidCerts()
	config := parseFlags()

	initLogger(config.LogType)

	if config.ServeUI {
		ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)

		if err := web.Serve(ctx, &web.Options{Listen: config.ListenAddr, Bench: config.benchOptions(), Filter: config.filter()}); err != nil {
			slog.ErrorContext(ctx, "UI server failed", slog.Any("err", err))
			cancel()
			os.Exit(1)
		}
		cancel()
		return
	}

	if config.List {
		if err := listServers(os.Stdout, config); err != nil {
			slog.ErrorContext(ctx, "Listing resolvers failed", slog.Any("err", err))
			os.Exit(1)
		}
		return
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "Starting", slog.Any("config", fmt.Sprintf("%#v", config)))
	if err := run(ctx, config); err != nil {
		if errors.Is(err, errInterrupted) {
			// 128 + SIGINT, as a shell reports a process that Ctrl+C ended.
			os.Exit(130)
		}
		slog.ErrorContext(ctx, "Benchmark failed", slog.Any("err", err))
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
