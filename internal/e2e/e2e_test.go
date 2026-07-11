//go:build integration

// Package e2e holds end-to-end integration tests that wire the real master agent-gateway and a
// real agent together over the actual mTLS gRPC transport, backed by a real Postgres. It requires
// ORKESTRA_TEST_DATABASE_URL to be set (a throwaway database); otherwise the tests skip.
package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/heckertobias/orkestra/internal/agent/metrics" // register orkestra_agent_* metrics

	"github.com/heckertobias/orkestra/internal/agent/conn"
	"github.com/heckertobias/orkestra/internal/agent/enroll"
	"github.com/heckertobias/orkestra/internal/master/agentgw"
	"github.com/heckertobias/orkestra/internal/master/pki"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
	"github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1/orkestrav1connect"
)

// TestEnrollAndFederateMetrics exercises the full master↔agent path end to end:
// bootstrap token → mTLS enrollment on the agent gateway → persistent Connect stream →
// federated metrics fetched by the master over that stream.
func TestEnrollAndFederateMetrics(t *testing.T) {
	dsn := os.Getenv("ORKESTRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ORKESTRA_TEST_DATABASE_URL to a throwaway Postgres to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// --- Database (migrations run inside store.Open) ---
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	// --- CA (fixed KEK so re-runs against the same DB reuse the persisted CA) ---
	kek := bytes.Repeat([]byte{1}, 32)
	ca, err := pki.LoadOrCreate(ctx, db, kek)
	if err != nil {
		t.Fatalf("pki.LoadOrCreate: %v", err)
	}

	// --- Agent gateway over a real mTLS listener (mirrors cmd/orkestra-master/main.go) ---
	serverCert, err := ca.IssueServerCert([]string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback})
	if err != nil {
		t.Fatalf("IssueServerCert: %v", err)
	}
	tlsCfg := agentgw.NewAgentTLSConfig(serverCert, ca.CertPool())

	registry := agentgw.NewRegistry()
	h := agentgw.NewHandler(db, ca, registry, nil)

	path, svc := orkestrav1connect.NewAgentServiceHandler(h)
	mux := http.NewServeMux()
	noRevocation := func(context.Context, string) (bool, error) { return false, nil }
	mux.Handle(path, agentgw.MTLSMiddleware(noRevocation, svc))

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	var protos http.Protocols
	protos.SetHTTP1(true)
	protos.SetHTTP2(true)
	srv := &http.Server{Handler: mux, TLSConfig: tlsCfg, Protocols: &protos}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	masterAddr := "https://" + ln.Addr().String()

	// --- Mint a bootstrap token and enroll a real agent ---
	rawToken, _, err := pki.CreateEnrollmentToken(ctx, db, "e2e", time.Hour, 1, nil)
	if err != nil {
		t.Fatalf("CreateEnrollmentToken: %v", err)
	}

	dir := t.TempDir()
	enrollCtx, cancelEnroll := context.WithTimeout(ctx, 30*time.Second)
	err = enroll.Run(enrollCtx, enroll.Params{
		MasterAddr:     masterAddr,
		BootstrapToken: rawToken,
		ServerName:     "e2e-agent",
		DataDir:        dir,
	})
	cancelEnroll()
	if err != nil {
		t.Fatalf("enroll.Run: %v", err)
	}

	cfg, err := enroll.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AgentID == "" {
		t.Fatal("empty agent id after enrollment")
	}

	// --- Start the agent's persistent Connect stream ---
	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()
	agent := conn.New(cfg, dir, func(context.Context, *orkestraV1.MasterMessage) error { return nil })
	go agent.RunForever(agentCtx)

	// --- Wait for the agent session to register ---
	if !waitFor(5*time.Second, func() bool { return registry.Get(cfg.AgentID) != nil }) {
		t.Fatal("agent did not connect within timeout")
	}

	// --- Fetch federated metrics over the stream ---
	fetchCtx, cancelFetch := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFetch()
	text, err := h.FetchAgentMetrics(fetchCtx, cfg.AgentID)
	if err != nil {
		t.Fatalf("FetchAgentMetrics: %v", err)
	}
	if !strings.Contains(text, "orkestra_agent_containers_running") {
		t.Fatalf("federated metrics missing expected agent metric; got %d bytes:\n%s", len(text), truncate(text, 800))
	}

	// --- The server row should be online ---
	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM servers WHERE id = $1`, cfg.AgentID).Scan(&status); err != nil {
		t.Fatalf("query server status: %v", err)
	}
	if status != "online" {
		t.Fatalf("server status = %q, want online", status)
	}

	// Graceful teardown: close the server first so the master processes the disconnect and writes
	// the offline status while the DB pool is still open, then wait for the session to unregister
	// (which happens after that write). This keeps the deferred db.Close() from racing the master's
	// offline write — avoiding a benign but noisy "closed pool" error in the test log.
	cancelAgent()
	_ = srv.Close()
	waitFor(3*time.Second, func() bool { return registry.Get(cfg.AgentID) == nil })
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
