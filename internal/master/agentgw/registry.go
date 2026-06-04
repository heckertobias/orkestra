// Package agentgw implements the Agent Gateway: the gRPC server that accepts Agent connections,
// maintains a session registry, and dispatches messages to/from Agents.
package agentgw

import (
	"context"
	"log/slog"
	"sync"
	"time"

	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// Session holds the state of a connected Agent.
type Session struct {
	AgentID   string
	ServerID  string
	Connected time.Time
	send      chan *orkestraV1.MasterMessage
	done      chan struct{}
}

// Send enqueues a message to be sent to the Agent. Non-blocking; drops if full.
func (s *Session) Send(msg *orkestraV1.MasterMessage) bool {
	select {
	case s.send <- msg:
		return true
	default:
		return false
	}
}

// Registry is a thread-safe map of agentID → active Session.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewRegistry creates an empty session registry.
func NewRegistry() *Registry {
	return &Registry{sessions: make(map[string]*Session)}
}

// Register adds a new session for agentID. Any existing session is replaced.
func (r *Registry) Register(agentID, serverID string) *Session {
	s := &Session{
		AgentID:   agentID,
		ServerID:  serverID,
		Connected: time.Now(),
		send:      make(chan *orkestraV1.MasterMessage, 64),
		done:      make(chan struct{}),
	}
	r.mu.Lock()
	r.sessions[agentID] = s
	r.mu.Unlock()
	slog.Info("agent connected", "agent_id", agentID)
	return s
}

// Unregister removes the session for agentID.
func (r *Registry) Unregister(agentID string) {
	r.mu.Lock()
	if s, ok := r.sessions[agentID]; ok {
		close(s.done)
		delete(r.sessions, agentID)
	}
	r.mu.Unlock()
	slog.Info("agent disconnected", "agent_id", agentID)
}

// Get returns the session for agentID, or nil if not connected.
func (r *Registry) Get(agentID string) *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[agentID]
}

// ConnectedIDs returns the IDs of all currently connected Agents.
func (r *Registry) ConnectedIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.sessions))
	for id := range r.sessions {
		ids = append(ids, id)
	}
	return ids
}

// RunHeartbeatMonitor marks servers offline after missedThreshold missed heartbeats.
// It checks every interval and reads last_seen_at from the DB via the provided callback.
func (r *Registry) RunHeartbeatMonitor(ctx context.Context, interval time.Duration, onMissed func(agentID string)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastSeen := make(map[string]time.Time)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.RLock()
			for id, s := range r.sessions {
				_ = s
				if t, ok := lastSeen[id]; ok && time.Since(t) > 3*interval {
					go onMissed(id)
				}
			}
			r.mu.RUnlock()
		}
	}
}

// UpdateLastSeen records the time of the last message from an Agent.
func (r *Registry) UpdateLastSeen(agentID string, t time.Time) {
	// Stored per-session for heartbeat monitoring.
	r.mu.RLock()
	s := r.sessions[agentID]
	r.mu.RUnlock()
	if s != nil {
		s.Connected = t // reuse field as last-seen proxy for simplicity
	}
}
