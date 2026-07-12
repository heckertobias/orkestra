//go:build integration

package agentgw

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// TestHandleStatusReportPersistsAvailableUpdates verifies the master plumbing: a StatusReport
// carrying available_updates is upserted into the available_updates table by handleStatusReport.
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

	agentID := "handler-status-server-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := db.Exec(ctx, `
		INSERT INTO servers (id, name, hostname, arch, os, status, enrolled_at)
		VALUES ($1, $1, $1, 'amd64', 'linux', 'offline', $2)`,
		agentID, time.Now().UnixMilli(),
	); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	h := NewHandler(db, nil, NewRegistry(), nil)

	h.handleStatusReport(ctx, agentID, &orkestraV1.StatusReport{
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
