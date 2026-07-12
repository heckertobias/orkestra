package agentgw

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heckertobias/orkestra/internal/master/pki"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

const certTTL = 365 * 24 * time.Hour

// EventFn is called to persist a system event.
type EventFn func(ctx context.Context, p store.InsertEventParams)

// Handler implements AgentServiceHandler (the Connect/gRPC server side).
type Handler struct {
	db       *pgxpool.Pool
	ca       *pki.CA
	registry *Registry
	q        *store.Queries
	emitFn   EventFn
}

// NewHandler creates an AgentService handler.
func NewHandler(db *pgxpool.Pool, ca *pki.CA, registry *Registry, emitFn EventFn) *Handler {
	return &Handler{db: db, ca: ca, registry: registry, q: store.New(db), emitFn: emitFn}
}

func (h *Handler) emit(ctx context.Context, serverID *string, eventType, severity, message string) {
	if h.emitFn == nil {
		return
	}
	h.emitFn(ctx, store.InsertEventParams{
		Ts:        time.Now().UnixMilli(),
		ServerID:  serverID,
		EventType: eventType,
		Severity:  severity,
		Message:   message,
	})
}

// Enroll handles one-time Agent enrollment: validates the bootstrap token, signs the CSR,
// creates a server record, and persists the certificate.
func (h *Handler) Enroll(ctx context.Context, req *connect.Request[orkestraV1.EnrollRequest]) (*connect.Response[orkestraV1.EnrollResponse], error) {
	r := req.Msg
	if r.BootstrapToken == "" || r.CsrPem == "" || r.NodeInfo == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("bootstrap_token, csr_pem, and node_info are required"))
	}

	// Validate token.
	_, err := pki.ValidateEnrollmentToken(ctx, h.db, r.BootstrapToken)
	if err != nil {
		slog.Warn("enrollment token validation failed", "err", err)
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("invalid or expired bootstrap token"))
	}

	// Assign the agent identity (used as the DB server id AND the client-cert CN, so every
	// subsequent RPC — identified by cert CN — maps back to this row). The master assigns the
	// CN; the agent does not get to choose its own identity via the CSR subject.
	agentID := uuid.NewString()

	// Sign CSR with the master-assigned CN.
	certPEM, serial, err := h.ca.SignCSRWithCN(r.CsrPem, agentID, certTTL)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("sign CSR: %w", err))
	}
	fingerprint, err := pki.CertFingerprint(certPEM)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Create or update server record.
	now := time.Now().UnixMilli()
	info := r.NodeInfo

	hostname := info.Hostname
	if hostname == "" {
		hostname = "unknown"
	}
	name := hostname
	if info.Hostname != "" {
		name = info.Hostname
	}

	_, err = h.db.Exec(ctx, `
		INSERT INTO servers (id, name, hostname, arch, os, agent_version, docker_version, labels, status, enrolled_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', 'offline', $8)`,
		agentID, name, hostname, info.Arch, info.Os,
		info.AgentVersion, info.DockerVersion, now,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create server record: %w", err))
	}

	// Persist certificate.
	notBefore := time.Now().Add(-time.Minute).UnixMilli()
	notAfter := time.Now().Add(certTTL).UnixMilli()
	_, err = h.db.Exec(ctx, `
		INSERT INTO certificates (serial, agent_id, fingerprint, cert_pem, not_before, not_after, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		serial, agentID, fingerprint, certPEM, notBefore, notAfter, now,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("persist certificate: %w", err))
	}

	slog.Info("agent enrolled",
		"agent_id", agentID,
		"hostname", hostname,
		"arch", info.Arch,
	)

	return connect.NewResponse(&orkestraV1.EnrollResponse{
		AgentId:       agentID,
		ClientCertPem: certPEM,
		CaBundlePem:   h.ca.CertPEM(),
	}), nil
}

// Connect handles the persistent bidi-stream after mTLS enrollment.
func (h *Handler) Connect(ctx context.Context, stream *connect.BidiStream[orkestraV1.AgentMessage, orkestraV1.MasterMessage]) error {
	// Extract agentID from the mTLS client certificate CN.
	agentID, err := agentIDFromContext(ctx)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}

	session := h.registry.Register(agentID, agentID)
	defer h.registry.Unregister(agentID)

	// Mark server online.
	h.setServerStatus(ctx, agentID, "online")
	sid := agentID
	h.emit(ctx, &sid, "agent", "info", "agent connected")

	// Send loop: forward queued MasterMessages to the Agent.
	sendErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				sendErr <- nil
				return
			case <-session.done:
				sendErr <- nil
				return
			case msg := <-session.send:
				if err := stream.Send(msg); err != nil {
					sendErr <- err
					return
				}
			}
		}
	}()

	// Receive loop: process AgentMessages.
	for {
		msg, err := stream.Receive()
		if err != nil {
			break
		}
		h.registry.UpdateLastSeen(agentID, time.Now())
		h.handleAgentMessage(ctx, agentID, msg)
	}

	sid2 := agentID
	h.emit(context.Background(), &sid2, "agent", "warn", "agent disconnected")
	h.setServerStatus(context.Background(), agentID, "offline")
	return nil
}

// RenewCert handles certificate renewal for an already-enrolled Agent.
func (h *Handler) RenewCert(ctx context.Context, req *connect.Request[orkestraV1.RenewCertRequest]) (*connect.Response[orkestraV1.RenewCertResponse], error) {
	agentID, err := agentIDFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// Re-sign with the same master-assigned CN so the renewed cert keeps the agent identity.
	certPEM, serial, err := h.ca.SignCSRWithCN(req.Msg.CsrPem, agentID, certTTL)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("sign CSR: %w", err))
	}
	fingerprint, err := pki.CertFingerprint(certPEM)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	now := time.Now().UnixMilli()
	_, err = h.db.Exec(ctx, `
		INSERT INTO certificates (serial, agent_id, fingerprint, cert_pem, not_before, not_after, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		serial, agentID, fingerprint, certPEM,
		time.Now().Add(-time.Minute).UnixMilli(), time.Now().Add(certTTL).UnixMilli(), now,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("persist renewed cert: %w", err))
	}

	slog.Info("agent cert renewed", "agent_id", agentID)
	return connect.NewResponse(&orkestraV1.RenewCertResponse{
		ClientCertPem: certPEM,
		CaBundlePem:   h.ca.CertPEM(),
	}), nil
}

func (h *Handler) handleAgentMessage(ctx context.Context, agentID string, msg *orkestraV1.AgentMessage) {
	switch p := msg.Payload.(type) {
	case *orkestraV1.AgentMessage_Hello:
		h.handleHello(ctx, agentID, p.Hello)
	case *orkestraV1.AgentMessage_StatusReport:
		h.handleStatusReport(ctx, agentID, p.StatusReport)
	case *orkestraV1.AgentMessage_MetricsResponse:
		if s := h.registry.Get(agentID); s != nil {
			s.deliverResponse(msg)
		}
	case *orkestraV1.AgentMessage_Pong:
		// heartbeat acknowledged
	default:
		slog.Debug("unhandled agent message type", "agent_id", agentID)
	}
}

// ErrAgentNotConnected is returned when a request targets an agent that has no active session.
var ErrAgentNotConnected = errors.New("agent not connected")

// FetchAgentMetrics asks the connected agent for its current Prometheus metrics over the mTLS
// stream and returns the exposition-format text. It blocks until the agent replies or ctx expires,
// letting the Master federate per-agent metrics without any inbound port on the agent host.
func (h *Handler) FetchAgentMetrics(ctx context.Context, agentID string) (string, error) {
	s := h.registry.Get(agentID)
	if s == nil {
		return "", ErrAgentNotConnected
	}
	reqID := uuid.NewString()
	ch := s.awaitResponse(reqID)
	defer s.cancelResponse(reqID)

	if !s.Send(&orkestraV1.MasterMessage{
		RequestId: reqID,
		Payload:   &orkestraV1.MasterMessage_MetricsRequest{MetricsRequest: &orkestraV1.MetricsRequest{}},
	}) {
		return "", errors.New("agent send queue full")
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resp := <-ch:
		mr := resp.GetMetricsResponse()
		if mr == nil {
			return "", errors.New("unexpected agent response")
		}
		if mr.Error != "" {
			return "", fmt.Errorf("agent metrics error: %s", mr.Error)
		}
		return mr.PrometheusText, nil
	}
}

func (h *Handler) handleHello(ctx context.Context, agentID string, hello *orkestraV1.Hello) {
	now := time.Now().UnixMilli()
	_, err := h.db.Exec(ctx, `
		UPDATE servers SET
			agent_version  = $1,
			docker_version = $2,
			hostname       = $3,
			arch           = $4,
			os             = $5,
			status         = 'online',
			last_seen_at   = $6
		WHERE id = $7`,
		hello.AgentVersion, hello.DockerVersion, hello.Hostname,
		hello.Arch, hello.Os, now, agentID,
	)
	if err != nil {
		slog.Error("update server from Hello", "agent_id", agentID, "err", err)
	}
	slog.Info("agent hello received", "agent_id", agentID, "hostname", hello.Hostname)
}

// IngestStatusReport applies a StatusReport as if it had arrived over the Connect
// stream. It exists so integration tests can exercise the status-report persistence
// path (including available_updates) without driving a full agent connection.
func (h *Handler) IngestStatusReport(ctx context.Context, agentID string, report *orkestraV1.StatusReport) {
	h.handleStatusReport(ctx, agentID, report)
}

func (h *Handler) handleStatusReport(ctx context.Context, agentID string, report *orkestraV1.StatusReport) {
	now := time.Now().UnixMilli()
	_, err := h.db.Exec(ctx,
		`UPDATE servers SET status = 'online', last_seen_at = $1 WHERE id = $2`,
		now, agentID,
	)
	if err != nil {
		slog.Error("update server from StatusReport", "agent_id", agentID, "err", err)
	}

	// Persist any agent-reported available updates (best-effort). Foundation only:
	// this records "an update is available" per (server, layer); no apply logic here.
	for _, u := range report.AvailableUpdates {
		if _, err := h.q.UpsertAvailableUpdate(ctx, store.UpsertAvailableUpdateParams{
			ServerID:         agentID,
			Layer:            u.Layer,
			CurrentVersion:   u.Current,
			CandidateVersion: u.Candidate,
			Detail:           []byte("{}"),
			DetectedAt:       now,
		}); err != nil {
			slog.Error("persist available update", "agent_id", agentID, "layer", u.Layer, "err", err)
		}
	}

	slog.Debug("status report received",
		"agent_id", agentID,
		"stacks", len(report.Stacks),
		"available_updates", len(report.AvailableUpdates),
	)
}

func (h *Handler) setServerStatus(ctx context.Context, agentID, status string) {
	now := time.Now().UnixMilli()
	_, err := h.db.Exec(ctx,
		`UPDATE servers SET status = $1, last_seen_at = $2 WHERE id = $3`,
		status, now, agentID,
	)
	if err != nil {
		slog.Error("set server status", "agent_id", agentID, "status", status, "err", err)
	}
}

// agentIDFromContext extracts the Agent ID from the mTLS client certificate CN.
func agentIDFromContext(ctx context.Context) (string, error) {
	// ConnectRPC provides TLS peer info via the request's context in the http.Request.
	// For now we extract from context value set by TLS middleware.
	if id, ok := ctx.Value(agentIDKey{}).(string); ok && id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no agent ID in mTLS certificate")
}

type agentIDKey struct{}

// WithAgentID returns a context carrying the agent ID extracted from the mTLS cert.
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, agentIDKey{}, agentID)
}
