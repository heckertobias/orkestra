package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/heckertobias/orkestra/internal/shared/version"
)

func main() {
	var (
		uiAddr    = flag.String("ui-addr", envOrDefault("ORKESTRA_UI_ADDR", "0.0.0.0:8080"), "UI & API listen address")
		agentAddr = flag.String("agent-addr", envOrDefault("ORKESTRA_AGENT_ADDR", "0.0.0.0:8443"), "Agent gRPC listen address")
		logLevel  = flag.String("log-level", envOrDefault("ORKESTRA_LOG_LEVEL", "info"), "Log level (debug|info|warn|error)")
	)
	flag.Parse()

	setupLogger(*logLevel)

	slog.Info("orkestra master starting",
		"version", version.Version,
		"commit", version.Commit,
		"build_date", version.BuildDate,
		"ui_addr", *uiAddr,
		"agent_addr", *agentAddr,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- UI / API server ---
	uiMux := http.NewServeMux()
	uiMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	uiMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		// TODO: add DB + CA readiness checks in M1
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	uiServer := &http.Server{
		Addr:        *uiAddr,
		Handler:     uiMux,
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	go func() {
		slog.Info("UI server listening", "addr", *uiAddr)
		if err := uiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("UI server error", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := uiServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("UI server shutdown error", "err", err)
	}

	slog.Info("goodbye")
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
