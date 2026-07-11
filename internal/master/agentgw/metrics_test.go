package agentgw

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// simulateAgentReply reads the MetricsRequest off the session's send queue and delivers reply.
func simulateAgentReply(sess *Session, reply *orkestraV1.MetricsResponse) {
	go func() {
		msg := <-sess.send
		sess.deliverResponse(&orkestraV1.AgentMessage{
			RequestId: msg.RequestId,
			Payload:   &orkestraV1.AgentMessage_MetricsResponse{MetricsResponse: reply},
		})
	}()
}

func TestFetchAgentMetrics_RoundTrip(t *testing.T) {
	reg := NewRegistry()
	sess := reg.Register("agent-1", "agent-1")
	h := &Handler{registry: reg}

	simulateAgentReply(sess, &orkestraV1.MetricsResponse{PrometheusText: "orkestra_test 1\n"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	text, err := h.FetchAgentMetrics(ctx, "agent-1")
	if err != nil {
		t.Fatalf("FetchAgentMetrics: %v", err)
	}
	if text != "orkestra_test 1\n" {
		t.Fatalf("unexpected metrics text: %q", text)
	}
}

func TestFetchAgentMetrics_NotConnected(t *testing.T) {
	h := &Handler{registry: NewRegistry()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := h.FetchAgentMetrics(ctx, "missing"); !errors.Is(err, ErrAgentNotConnected) {
		t.Fatalf("expected ErrAgentNotConnected, got %v", err)
	}
}

func TestFetchAgentMetrics_AgentError(t *testing.T) {
	reg := NewRegistry()
	sess := reg.Register("agent-2", "agent-2")
	h := &Handler{registry: reg}

	simulateAgentReply(sess, &orkestraV1.MetricsResponse{Error: "boom"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := h.FetchAgentMetrics(ctx, "agent-2"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected agent error containing boom, got %v", err)
	}
}

func TestFetchAgentMetrics_Timeout(t *testing.T) {
	reg := NewRegistry()
	reg.Register("agent-3", "agent-3") // no agent reply
	h := &Handler{registry: reg}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := h.FetchAgentMetrics(ctx, "agent-3"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
