//go:build integration

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/heckertobias/orkestra/internal/master/agentgw"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// TestHandleStatusReportPersistsInventory verifies the Master plumbing behind #29: the container
// inventory in a StatusReport lands in agent_state and round-trips back out of the JSONB blob,
// which is what the UI read and container→stack resolution both depend on.
func TestHandleStatusReportPersistsInventory(t *testing.T) {
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

	agentID := "agent-state-server-" + uniqueSuffix()
	insertTestServer(ctx, t, db, agentID)

	h := agentgw.NewHandler(db, nil, agentgw.NewRegistry(), nil)

	report := &orkestraV1.StatusReport{
		ReportedAtMs: time.Now().UnixMilli(),
		Stacks: []*orkestraV1.StackStatus{
			{
				StackId:        "stack-a",
				RunningVersion: "ver-a1",
				Containers: []*orkestraV1.ContainerStatus{
					{
						ContainerId:  "container-1",
						Name:         "stack-a-web-1234abcd",
						Image:        "nginx:1.27",
						ServiceName:  "web",
						State:        "running",
						Status:       "Up 3 hours",
						Health:       "healthy",
						RestartCount: 2,
						StartedAtMs:  time.Now().Add(-3 * time.Hour).UnixMilli(),
					},
				},
			},
			{
				StackId:       "stack-b",
				Error:         "converge stack-b: 1 service(s) failed",
				ServiceErrors: []*orkestraV1.ServiceError{{ServiceName: "api", Error: "pull access denied"}},
			},
		},
	}
	h.IngestStatusReport(ctx, agentID, report)

	row, err := q.GetAgentState(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgentState: %v", err)
	}
	if row.UpdatedAt == 0 {
		t.Fatal("agent_state.updated_at must be set")
	}

	var got orkestraV1.StatusReport
	if err := protojson.Unmarshal(row.StateJson, &got); err != nil {
		t.Fatalf("decode stored agent state: %v", err)
	}
	if len(got.Stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(got.Stacks))
	}

	a := got.Stacks[0]
	if a.StackId != "stack-a" || a.RunningVersion != "ver-a1" || len(a.Containers) != 1 {
		t.Fatalf("stack-a did not round-trip: %+v", a)
	}
	c := a.Containers[0]
	if c.ContainerId != "container-1" || c.ServiceName != "web" || c.Health != "healthy" || c.RestartCount != 2 {
		t.Fatalf("container did not round-trip: %+v", c)
	}

	b := got.Stacks[1]
	if len(b.ServiceErrors) != 1 || b.ServiceErrors[0].ServiceName != "api" {
		t.Fatalf("per-service errors did not round-trip: %+v", b.ServiceErrors)
	}

	// A second report replaces the row: the inventory is a full snapshot of the host, so a stack
	// that has disappeared must not linger.
	h.IngestStatusReport(ctx, agentID, &orkestraV1.StatusReport{ReportedAtMs: time.Now().UnixMilli()})

	row, err = q.GetAgentState(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgentState (second report): %v", err)
	}
	got.Reset()
	if err := protojson.Unmarshal(row.StateJson, &got); err != nil {
		t.Fatalf("decode stored agent state (second report): %v", err)
	}
	if len(got.Stacks) != 0 {
		t.Fatalf("expected the replaced report to carry no stacks, got %d", len(got.Stacks))
	}
}
