package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/egt/orkestra/internal/shared/version"
)

func main() {
	// Subcommands: serve | enroll
	if len(os.Args) < 2 {
		slog.Error("usage: orkestra-agent <serve|enroll> [flags]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "enroll":
		runEnroll(os.Args[2:])
	default:
		slog.Error("unknown subcommand", "subcommand", os.Args[1])
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var (
		dataDir  = fs.String("data-dir", envOrDefault("DOCKESTRA_AGENT_DATA", "/etc/orkestra/agent"), "Agent data directory")
		logLevel = fs.String("log-level", envOrDefault("DOCKESTRA_LOG_LEVEL", "info"), "Log level")
	)
	_ = fs.Parse(args)

	setupLogger(*logLevel)

	slog.Info("orkestra agent starting",
		"version", version.Version,
		"commit", version.Commit,
		"data_dir", *dataDir,
	)

	// TODO M1: load config, enroll if not already enrolled, start gRPC connection
	slog.Info("agent waiting — not yet enrolled or connected (M1)")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	slog.Info("agent shutting down")
}

func runEnroll(args []string) {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	var (
		masterAddr     = fs.String("master", "", "Master address (required)")
		bootstrapToken = fs.String("bootstrap-token", "", "Bootstrap token (required)")
		name           = fs.String("name", "", "Human-readable server name")
		dataDir        = fs.String("data-dir", envOrDefault("DOCKESTRA_AGENT_DATA", "/etc/orkestra/agent"), "Agent data directory")
		logLevel       = fs.String("log-level", envOrDefault("DOCKESTRA_LOG_LEVEL", "info"), "Log level")
	)
	_ = fs.Parse(args)

	setupLogger(*logLevel)

	if *masterAddr == "" || *bootstrapToken == "" {
		slog.Error("--master and --bootstrap-token are required")
		os.Exit(1)
	}

	slog.Info("starting enrollment",
		"master", *masterAddr,
		"name", *name,
		"data_dir", *dataDir,
	)

	// TODO M1: implement enrollment (keypair gen, CSR, EnrollRPC, cert persistence)
	slog.Info("enrollment not yet implemented — coming in M1")
	os.Exit(1)
}

func setupLogger(level string) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	handler := slog.NewTextHandler(os.Stderr, opts)
	slog.SetDefault(slog.New(handler))
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
