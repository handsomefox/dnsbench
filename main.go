package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/phsym/console-slog"
)

func main() {
	config := parseFlags()

	level := slog.LevelInfo
	if config.Verbose {
		level = slog.LevelDebug
	}

	logger := slog.New(
		console.NewHandler(os.Stderr, &console.HandlerOptions{
			Level: level,
		}),
	)
	slog.SetDefault(logger)

	slog.Debug("Starting", slog.Any("config", fmt.Sprintf("%#v", config)))
	if err := run(config); err != nil {
		slog.Error("Benchmark failed", slogErr(err))
		os.Exit(1)
	}
}
