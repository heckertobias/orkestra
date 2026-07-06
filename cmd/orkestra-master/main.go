package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/heckertobias/orkestra/internal/master/agentgw"
	masterapi "github.com/heckertobias/orkestra/internal/master/api"
	masterauth "github.com/heckertobias/orkestra/internal/master/auth"
	masteremail "github.com/heckertobias/orkestra/internal/master/email"
	"github.com/heckertobias/orkestra/internal/master/keys"
	masteroidc "github.com/heckertobias/orkestra/internal/master/oidc"
	"github.com/heckertobias/orkestra/internal/master/pki"
	masterreconciler "github.com/heckertobias/orkestra/internal/master/reconciler"
	"github.com/heckertobias/orkestra/internal/master/store"
	"github.com/heckertobias/orkestra/internal/shared/version"
	"github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1/orkestrav1connect"
	webui "github.com/heckertobias/orkestra/web"
)

// publicProcedures lists Connect RPC procedures that do not require a session.
var publicProcedures = map[string]bool{
	orkestrav1connect.AuthServiceLoginProcedure:                true,
	orkestrav1connect.AuthServiceRequestPasswordResetProcedure: true,
	orkestrav1connect.AuthServiceResetPasswordWithTokenProcedure: true,
	orkestrav1connect.AuthServiceConfirmEmailChangeProcedure:    true,
}

func main() {
	var (
		uiAddr      = flag.String("ui-addr", envOrDefault("ORKESTRA_UI_ADDR", "0.0.0.0:8080"), "UI & API listen address")
		agentAddr   = flag.String("agent-addr", envOrDefault("ORKESTRA_AGENT_ADDR", "0.0.0.0:8443"), "Agent gRPC listen address")
		metricsAddr = flag.String("metrics-addr", envOrDefault("ORKESTRA_METRICS_ADDR", "0.0.0.0:9090"), "Prometheus metrics listen address")
		dbURL       = flag.String("db", envOrDefault("ORKESTRA_DATABASE_URL", ""), "PostgreSQL DSN (required)")
		logLevel    = flag.String("log-level", envOrDefault("ORKESTRA_LOG_LEVEL", "info"), "Log level (debug|info|warn|error)")
	)
	flag.Parse()

	setupLogger(*logLevel)

	slog.Info("orkestra master starting",
		"version", version.Version,
		"commit", version.Commit,
		"build_date", version.BuildDate,
		"ui_addr", *uiAddr,
		"agent_addr", *agentAddr,
		"metrics_addr", *metricsAddr,
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

	// --- First-run setup ---
	q := store.New(db)
	var setupToken string
	count, err := q.CountUsers(ctx)
	if err != nil {
		slog.Error("count users", "err", err)
		os.Exit(1)
	}
	if count == 0 {
		tok, err := masterauth.GenerateSetupToken()
		if err != nil {
			slog.Error("generate setup token", "err", err)
			os.Exit(1)
		}
		setupToken = tok
		uiURL := fmt.Sprintf("http://%s", *uiAddr)
		slog.Warn("FIRST RUN: no users configured — open setup URL to create the admin account",
			"url", fmt.Sprintf("%s/login?setup=%s", uiURL, setupToken))
	}

	// --- Event emitter ---
	emitFn := agentgw.EventFn(func(ctx context.Context, p store.InsertEventParams) {
		go func() {
			_ = q.InsertEvent(context.Background(), p)
		}()
	})

	// --- Agent Gateway ---
	registry := agentgw.NewRegistry()
	gwHandler := agentgw.NewHandler(db, ca, registry, emitFn)

	revocationChecker := agentgw.RevocationChecker(func(rCtx context.Context, fingerprint string) (bool, error) {
		cert, err := q.GetCertificateByFingerprint(rCtx, fingerprint)
		if err != nil {
			return false, nil // treat lookup errors as non-revoked to avoid bricking agents on DB hiccup
		}
		return cert.Revoked, nil
	})

	agentMux := http.NewServeMux()
	agentPath, agentSvcHandler := orkestrav1connect.NewAgentServiceHandler(gwHandler,
		connect.WithCompressMinBytes(1024),
	)
	agentMux.Handle(agentPath, agentgw.MTLSMiddleware(revocationChecker, agentSvcHandler))

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

	// Offline detection.
	go registry.RunHeartbeatMonitor(ctx, 30*time.Second, func(agentID string) {
		_, err := db.Exec(context.Background(),
			`UPDATE servers SET status = 'offline' WHERE id = $1 AND status = 'online'`, agentID)
		if err != nil {
			slog.Error("mark server offline", "agent_id", agentID, "err", err)
		} else {
			slog.Info("server marked offline (missed heartbeats)", "agent_id", agentID)
		}
	})

	// --- Master Reconciler ---
	rec := masterreconciler.New(db, registry, 15*time.Second)
	go rec.Run(ctx)

	// --- OIDC Provider ---
	// The callback host must be browser-reachable: a 0.0.0.0 bind address is valid
	// for listening but never as a redirect target (Safari rejects it outright), so
	// normalise it to localhost.
	oidcCallbackAddr := *uiAddr
	if host, port, err := net.SplitHostPort(oidcCallbackAddr); err == nil && (host == "" || host == "0.0.0.0" || host == "::") {
		oidcCallbackAddr = net.JoinHostPort("localhost", port)
	}
	oidcRedirectURL := fmt.Sprintf("http://%s/auth/oidc/callback", oidcCallbackAddr)
	oidcPostLogoutURL := fmt.Sprintf("http://%s/login", oidcCallbackAddr)
	oidcProvider := masteroidc.New(q, kek)
	if err := oidcProvider.Reload(ctx, oidcRedirectURL, oidcPostLogoutURL); err != nil {
		slog.Warn("OIDC provider init failed (non-fatal)", "err", err)
	}

	// --- Connect interceptors ---
	authInterceptor := masterauth.NewAuthInterceptor(publicProcedures)
	connectOpts := []connect.HandlerOption{
		connect.WithCompressMinBytes(1024),
		connect.WithInterceptors(authInterceptor),
	}

	// --- StackService ---
	stackHandler := masterapi.NewStackServiceHandler(db, registry, func() { rec.PushNow(ctx) })
	stackPath, stackSvcHandler := orkestrav1connect.NewStackServiceHandler(stackHandler, connectOpts...)

	// --- SecretService ---
	secretHandler := masterapi.NewSecretServiceHandler(db, kek)
	secretPath, secretSvcHandler := orkestrav1connect.NewSecretServiceHandler(secretHandler, connectOpts...)

	// --- Email/SMTP ---
	mailer := masteremail.New(q, kek)

	// --- AuthService ---
	reloadOIDC := func(ctx context.Context) error { return oidcProvider.Reload(ctx, oidcRedirectURL, oidcPostLogoutURL) }
	authHandler := masterapi.NewAuthServiceHandler(db, kek, &setupToken, mailer, reloadOIDC, oidcProvider.LogoutURL)
	authPath, authSvcHandler := orkestrav1connect.NewAuthServiceHandler(authHandler, connectOpts...)

	// --- Session middleware ---
	sessionMW := masterauth.SessionMiddleware(q)

	// --- UI / API server ---
	uiMux := http.NewServeMux()
	uiMux.Handle(stackPath, stackSvcHandler)
	uiMux.Handle(secretPath, secretSvcHandler)
	uiMux.Handle(authPath, authSvcHandler)
	uiMux.HandleFunc("/api/setup", masterauth.RateLimitMiddleware(authHandler.SetupHTTPHandler))
	uiMux.HandleFunc("/api/audit", authHandler.AuditLogHTTPHandler)
	uiMux.HandleFunc("/auth/oidc/status", oidcProvider.StatusHandler)
	uiMux.HandleFunc("/auth/oidc/login", oidcProvider.LoginHandler)
	uiMux.HandleFunc("/auth/oidc/callback", oidcProvider.CallbackHandler(q, 24*time.Hour))
	uiMux.Handle("/", webui.Handler())
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

	var uiProtos http.Protocols
	uiProtos.SetHTTP1(true)
	uiProtos.SetHTTP2(true)
	uiServer := &http.Server{
		Addr:        *uiAddr,
		Handler:     sessionMW(uiMux),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
		Protocols:   &uiProtos,
	}

	go func() {
		slog.Info("UI server listening", "addr", *uiAddr)
		if err := uiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("UI server error", "err", err)
			os.Exit(1)
		}
	}()

	// --- Metrics server ---
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsServer := &http.Server{
		Addr:    *metricsAddr,
		Handler: metricsMux,
	}
	go func() {
		slog.Info("metrics server listening", "addr", *metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = uiServer.Shutdown(shutdownCtx)
	_ = agentServer.Shutdown(shutdownCtx)
	_ = metricsServer.Shutdown(shutdownCtx)
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
