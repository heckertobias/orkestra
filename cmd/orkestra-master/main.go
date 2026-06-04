package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/heckertobias/orkestra/internal/master/agentgw"
	"github.com/heckertobias/orkestra/internal/master/keys"
	"github.com/heckertobias/orkestra/internal/master/pki"
	"github.com/heckertobias/orkestra/internal/master/store"
	"github.com/heckertobias/orkestra/internal/shared/version"
	"github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1/orkestrav1connect"
)

func main() {
	var (
		uiAddr    = flag.String("ui-addr", envOrDefault("ORKESTRA_UI_ADDR", "0.0.0.0:8080"), "UI & API listen address")
		agentAddr = flag.String("agent-addr", envOrDefault("ORKESTRA_AGENT_ADDR", "0.0.0.0:8443"), "Agent gRPC listen address")
		dbURL     = flag.String("db", envOrDefault("ORKESTRA_DATABASE_URL", ""), "PostgreSQL DSN (required)")
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

	if *dbURL == "" {
		slog.Error("ORKESTRA_DATABASE_URL is required")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- KEK ---
	kek, err := keys.Load(ctx)
	if err != nil {
		slog.Error("load KEK", "err", err)
		os.Exit(1)
	}
	slog.Info("KEK loaded")

	// --- Database ---
	db, err := store.Open(ctx, *dbURL)
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("database connected and migrated")

	// --- PKI / CA ---
	ca, err := pki.LoadOrCreate(ctx, db, kek)
	if err != nil {
		slog.Error("load or create CA", "err", err)
		os.Exit(1)
	}
	slog.Info("CA ready")

	// --- Agent Gateway ---
	registry := agentgw.NewRegistry()
	gwHandler := agentgw.NewHandler(db, ca, registry)

	// Agent gRPC server (mTLS, HTTP/2).
	agentMux := http.NewServeMux()
	agentPath, agentSvcHandler := orkestrav1connect.NewAgentServiceHandler(gwHandler,
		connect.WithCompressMinBytes(1024),
	)
	agentMux.Handle(agentPath, agentgw.MTLSMiddleware(agentSvcHandler))

	// For the agent listener we use the CA cert as the server cert for simplicity —
	// in production operators would supply a real TLS cert via ORKESTRA_TLS_CERT/KEY.
	// The CA also verifies client certificates (mTLS).
	caCert := ca.TLSCert()
	agentTLSCfg := agentgw.NewAgentTLSConfig(caCert, ca.CertPool())

	agentServer := &http.Server{
		Addr:        *agentAddr,
		Handler:     agentMux,
		TLSConfig:   agentTLSCfg,
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	go func() {
		ln, err := tls.Listen("tcp", *agentAddr, agentTLSCfg)
		if err != nil {
			slog.Error("agent listener", "err", err)
			os.Exit(1)
		}
		slog.Info("agent gRPC server listening (mTLS)", "addr", *agentAddr)
		if err := agentServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("agent server error", "err", err)
		}
	}()

	// Offline detection: mark servers offline after 3 missed heartbeats (~90s).
	go registry.RunHeartbeatMonitor(ctx, 30*time.Second, func(agentID string) {
		_, err := db.Exec(context.Background(),
			`UPDATE servers SET status = 'offline' WHERE id = $1 AND status = 'online'`, agentID)
		if err != nil {
			slog.Error("mark server offline", "agent_id", agentID, "err", err)
		} else {
			slog.Info("server marked offline (missed heartbeats)", "agent_id", agentID)
		}
	})

	// --- UI / API server (plain HTTP/2 with h2c for dev, TLS in prod) ---
	uiMux := http.NewServeMux()
	uiMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	uiMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	uiServer := &http.Server{
		Addr:        *uiAddr,
		Handler:     h2c.NewHandler(uiMux, &http2.Server{}),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	go func() {
		slog.Info("UI server listening", "addr", *uiAddr)
		if err := uiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("UI server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = uiServer.Shutdown(shutdownCtx)
	_ = agentServer.Shutdown(shutdownCtx)
	slog.Info("goodbye")
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
