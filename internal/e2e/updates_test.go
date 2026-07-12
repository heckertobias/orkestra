//go:build integration

package e2e

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heckertobias/orkestra/internal/master/agentgw"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// uniqueSuffix returns a per-call unique string so parallel/repeat runs don't collide on
// server ids (the servers row is the FK target for update rows).
func uniqueSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// insertTestServer creates a minimal servers row so update rows can reference it via FK.
func insertTestServer(ctx context.Context, t *testing.T, db *pgxpool.Pool, id string) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO servers (id, name, hostname, arch, os, status, enrolled_at)
		VALUES ($1, $1, $1, 'amd64', 'linux', 'offline', $2)`,
		id, time.Now().UnixMilli(),
	)
	if err != nil {
		t.Fatalf("insert test server: %v", err)
	}
}

// TestUpdatePolicyResolution verifies the store round-trip for update policies:
// an agent-specific policy overrides the fleet default, and the fleet default is
// returned when no agent-specific row exists.
func TestUpdatePolicyResolution(t *testing.T) {
	dsn := os.Getenv("ORKESTRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ORKESTRA_TEST_DATABASE_URL to a throwaway Postgres to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	q := store.New(db)

	serverID := "update-policy-server-" + uniqueSuffix()
	insertTestServer(ctx, t, db, serverID)
	now := time.Now().UnixMilli()

	// Fleet default for the 'images' layer: manual.
	if _, err := q.UpsertFleetUpdatePolicy(ctx, store.UpsertFleetUpdatePolicyParams{
		Layer:     "images",
		Mode:      "manual",
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertFleetUpdatePolicy: %v", err)
	}

	// Before any agent-specific row, resolution falls back to the fleet default.
	got, err := q.ResolveUpdatePolicy(ctx, store.ResolveUpdatePolicyParams{
		ServerID: &serverID,
		Layer:    "images",
	})
	if err != nil {
		t.Fatalf("ResolveUpdatePolicy (fleet fallback): %v", err)
	}
	if got.ServerID != nil {
		t.Fatalf("expected fleet default (nil server_id), got server_id=%v", *got.ServerID)
	}
	if got.Mode != "manual" {
		t.Fatalf("fleet default mode = %q, want manual", got.Mode)
	}

	// Agent-specific override for the same layer: automatic.
	if _, err := q.UpsertAgentUpdatePolicy(ctx, store.UpsertAgentUpdatePolicyParams{
		ServerID:  &serverID,
		Layer:     "images",
		Mode:      "automatic",
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertAgentUpdatePolicy: %v", err)
	}

	// Now resolution must prefer the agent-specific row.
	got, err = q.ResolveUpdatePolicy(ctx, store.ResolveUpdatePolicyParams{
		ServerID: &serverID,
		Layer:    "images",
	})
	if err != nil {
		t.Fatalf("ResolveUpdatePolicy (agent override): %v", err)
	}
	if got.ServerID == nil || *got.ServerID != serverID {
		t.Fatalf("expected agent-specific row for %q, got %+v", serverID, got)
	}
	if got.Mode != "automatic" {
		t.Fatalf("agent-specific mode = %q, want automatic", got.Mode)
	}

	// Upserting the agent policy again must update in place, not duplicate.
	if _, err := q.UpsertAgentUpdatePolicy(ctx, store.UpsertAgentUpdatePolicyParams{
		ServerID:  &serverID,
		Layer:     "images",
		Mode:      "manual",
		UpdatedAt: now + 1,
	}); err != nil {
		t.Fatalf("UpsertAgentUpdatePolicy (update): %v", err)
	}
	got, err = q.ResolveUpdatePolicy(ctx, store.ResolveUpdatePolicyParams{
		ServerID: &serverID,
		Layer:    "images",
	})
	if err != nil {
		t.Fatalf("ResolveUpdatePolicy (after update): %v", err)
	}
	if got.Mode != "manual" {
		t.Fatalf("after re-upsert mode = %q, want manual", got.Mode)
	}
}

// TestAvailableUpdateRoundTrip verifies upsert + list for agent-reported available updates.
func TestAvailableUpdateRoundTrip(t *testing.T) {
	dsn := os.Getenv("ORKESTRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ORKESTRA_TEST_DATABASE_URL to a throwaway Postgres to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	q := store.New(db)

	serverID := "available-update-server-" + uniqueSuffix()
	insertTestServer(ctx, t, db, serverID)
	now := time.Now().UnixMilli()

	if _, err := q.UpsertAvailableUpdate(ctx, store.UpsertAvailableUpdateParams{
		ServerID:         serverID,
		Layer:            "orkestra",
		CurrentVersion:   "v1.0.0",
		CandidateVersion: "v1.1.0",
		Detail:           []byte("{}"),
		DetectedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAvailableUpdate: %v", err)
	}

	// Re-upsert the same (server, layer) with a newer candidate: must update in place.
	if _, err := q.UpsertAvailableUpdate(ctx, store.UpsertAvailableUpdateParams{
		ServerID:         serverID,
		Layer:            "orkestra",
		CurrentVersion:   "v1.0.0",
		CandidateVersion: "v1.2.0",
		Detail:           []byte("{}"),
		DetectedAt:       now + 1,
	}); err != nil {
		t.Fatalf("UpsertAvailableUpdate (update): %v", err)
	}

	list, err := q.ListAvailableUpdatesForServer(ctx, serverID)
	if err != nil {
		t.Fatalf("ListAvailableUpdatesForServer: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 available update, got %d", len(list))
	}
	if list[0].CandidateVersion != "v1.2.0" {
		t.Fatalf("candidate_version = %q, want v1.2.0", list[0].CandidateVersion)
	}

	// Delete and confirm it's gone.
	if err := q.DeleteAvailableUpdate(ctx, store.DeleteAvailableUpdateParams{
		ServerID: serverID,
		Layer:    "orkestra",
	}); err != nil {
		t.Fatalf("DeleteAvailableUpdate: %v", err)
	}
	list, err = q.ListAvailableUpdatesForServer(ctx, serverID)
	if err != nil {
		t.Fatalf("ListAvailableUpdatesForServer (after delete): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 available updates after delete, got %d", len(list))
	}
}

// TestHandleStatusReportPersistsAvailableUpdates verifies the master plumbing: a StatusReport
// carrying available_updates is upserted into the available_updates table by the handler.
func TestHandleStatusReportPersistsAvailableUpdates(t *testing.T) {
	dsn := os.Getenv("ORKESTRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ORKESTRA_TEST_DATABASE_URL to a throwaway Postgres to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	q := store.New(db)

	agentID := "handler-status-server-" + uniqueSuffix()
	insertTestServer(ctx, t, db, agentID)

	// nil CA/registry are fine: handleStatusReport only touches the DB.
	h := agentgw.NewHandler(db, nil, agentgw.NewRegistry(), nil)

	h.IngestStatusReport(ctx, agentID, &orkestraV1.StatusReport{
		ReportedAtMs: time.Now().UnixMilli(),
		AvailableUpdates: []*orkestraV1.AvailableUpdate{
			{Layer: "orkestra", Current: "v1.0.0", Candidate: "v1.1.0"},
			{Layer: "os", Current: "0 packages", Candidate: "5 packages"},
		},
	})

	list, err := q.ListAvailableUpdatesForServer(ctx, agentID)
	if err != nil {
		t.Fatalf("ListAvailableUpdatesForServer: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 persisted available updates, got %d", len(list))
	}

	byLayer := map[string]store.AvailableUpdate{}
	for _, u := range list {
		byLayer[u.Layer] = u
	}
	if u, ok := byLayer["orkestra"]; !ok || u.CandidateVersion != "v1.1.0" || u.CurrentVersion != "v1.0.0" {
		t.Fatalf("orkestra update not persisted correctly: %+v", byLayer["orkestra"])
	}
	if u, ok := byLayer["os"]; !ok || u.CandidateVersion != "5 packages" {
		t.Fatalf("os update not persisted correctly: %+v", byLayer["os"])
	}
}
