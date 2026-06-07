package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/heckertobias/orkestra/internal/agent/conn"
	"github.com/heckertobias/orkestra/internal/agent/dockerctl"
	"github.com/heckertobias/orkestra/internal/agent/enroll"
	_ "github.com/heckertobias/orkestra/internal/agent/metrics" // register metrics
	agentreconcile "github.com/heckertobias/orkestra/internal/agent/reconcile"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
	"github.com/heckertobias/orkestra/internal/shared/version"
)

func main() {
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
		dataDir     = fs.String("data-dir", envOrDefault("ORKESTRA_AGENT_DATA", "/etc/orkestra/agent"), "Agent data directory")
		logLevel    = fs.String("log-level", envOrDefault("ORKESTRA_LOG_LEVEL", "info"), "Log level")
		metricsAddr = fs.String("metrics-addr", envOrDefault("ORKESTRA_AGENT_METRICS_ADDR", "0.0.0.0:9091"), "Prometheus metrics listen address")
	)
	_ = fs.Parse(args)

	setupLogger(*logLevel)
	slog.Info("orkestra agent starting", "version", version.Version, "data_dir", *dataDir)

	if !enroll.IsEnrolled(*dataDir) {
		slog.Error("agent not enrolled — run 'orkestra-agent enroll' first")
		os.Exit(1)
	}

	cfg, err := enroll.LoadConfig(*dataDir)
	if err != nil {
		slog.Error("load agent config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Set up Docker client and reconciler.
	dc, err := dockerctl.New()
	if err != nil {
		slog.Warn("docker client unavailable (is Docker running?)", "err", err)
		dc = nil
	}
	_ = dc // used via reconciler below

	rec := agentreconcile.New(dcForReconcile(dc))

	go rec.Run(ctx)

	agent := conn.New(cfg, *dataDir, func(ctx context.Context, msg *orkestraV1.MasterMessage) error {
		switch p := msg.Payload.(type) {
		case *orkestraV1.MasterMessage_ApplyDesiredState:
			rec.Apply(p.ApplyDesiredState)
		case *orkestraV1.MasterMessage_Ping:
			slog.Debug("ping from master", "ts", p.Ping.TimestampMs)
		case *orkestraV1.MasterMessage_ExecCommand:
			slog.Debug("exec command received (M2 dockerctl)")
		default:
			slog.Debug("unhandled master message")
		}
		return nil
	})

	// Start metrics server.
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	go func() {
		slog.Info("agent metrics listening", "addr", *metricsAddr)
		srv := &http.Server{Addr: *metricsAddr, Handler: metricsMux}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Warn("agent metrics server error", "err", err)
		}
	}()

	slog.Info("connecting to master", "master", cfg.MasterAddr, "agent_id", cfg.AgentID)
	agent.RunForever(ctx)
	slog.Info("agent stopped")
}

func runEnroll(args []string) {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	var (
		masterAddr     = fs.String("master", "", "Master address, e.g. https://master.example.com:8443 (required)")
		bootstrapToken = fs.String("bootstrap-token", "", "Bootstrap token (required)")
		name           = fs.String("name", "", "Human-readable server name (defaults to hostname)")
		dataDir        = fs.String("data-dir", envOrDefault("ORKESTRA_AGENT_DATA", "/etc/orkestra/agent"), "Agent data directory")
		logLevel       = fs.String("log-level", envOrDefault("ORKESTRA_LOG_LEVEL", "info"), "Log level")
	)
	_ = fs.Parse(args)

	setupLogger(*logLevel)

	if *masterAddr == "" || *bootstrapToken == "" {
		slog.Error("--master and --bootstrap-token are required")
		os.Exit(1)
	}

	serverName := *name
	if serverName == "" {
		serverName, _ = os.Hostname()
	}

	slog.Info("enrolling agent", "master", *masterAddr, "name", serverName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := enroll.Run(ctx, enroll.Params{
		MasterAddr:     *masterAddr,
		BootstrapToken: *bootstrapToken,
		ServerName:     serverName,
		DataDir:        *dataDir,
	}); err != nil {
		slog.Error("enrollment failed", "err", err)
		os.Exit(1)
	}

	cfg, _ := enroll.LoadConfig(*dataDir)
	slog.Info("enrollment successful",
		"agent_id", cfg.AgentID,
		"data_dir", *dataDir,
	)
}

// dcForReconcile extracts the underlying *client.Client from our dockerctl wrapper.
// Returns nil if docker is not available (reconciler handles nil gracefully).
func dcForReconcile(dc *dockerctl.Client) *client.Client {
	if dc == nil {
		return nil
	}
	return dc.RawClient()
}

func setupLogger(level string) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
