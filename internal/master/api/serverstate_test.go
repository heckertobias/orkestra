package api

import (
	"testing"

	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

func testReport() *orkestraV1.StatusReport {
	return &orkestraV1.StatusReport{
		ReportedAtMs: 1700000000000,
		Stacks: []*orkestraV1.StackStatus{
			{
				StackId:        "stack-a",
				RunningVersion: "ver-a1",
				Containers: []*orkestraV1.ContainerStatus{
					{
						ContainerId:  "c1",
						Name:         "stack-a-web-1234abcd",
						Image:        "nginx:1.27",
						ServiceName:  "web",
						State:        "running",
						Status:       "Up 3 hours",
						Health:       "healthy",
						RestartCount: 2,
						StartedAtMs:  1699999999000,
					},
				},
			},
			{
				StackId: "stack-b",
				Error:   "converge stack-b: 1 service(s) failed",
				ServiceErrors: []*orkestraV1.ServiceError{
					{ServiceName: "api", Error: "pull access denied"},
				},
			},
		},
	}
}

func TestServerStateFromReportConvertsContainers(t *testing.T) {
	state := serverStateFromReport("srv-1", testReport(), nil)

	if state.ServerId != "srv-1" || state.ReportedAt != 1700000000000 {
		t.Fatalf("server id/timestamp not carried through: %+v", state)
	}
	if len(state.Stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(state.Stacks))
	}

	a := state.Stacks[0]
	if a.RunningVersionId != "ver-a1" {
		t.Fatalf("running version id = %q, want ver-a1", a.RunningVersionId)
	}
	// The numeric version is resolved from the database, not from the report.
	if a.RunningVersion != 0 {
		t.Fatalf("numeric version must stay unresolved here, got %d", a.RunningVersion)
	}
	if len(a.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(a.Containers))
	}
	c := a.Containers[0]
	if c.Id != "c1" || c.Name != "stack-a-web-1234abcd" || c.Image != "nginx:1.27" {
		t.Fatalf("container identity not carried through: %+v", c)
	}
	if c.ServiceName != "web" || c.State != "running" || c.Status != "Up 3 hours" {
		t.Fatalf("container state not carried through: %+v", c)
	}
	if c.Health != "healthy" || c.RestartCount != 2 || c.StartedAt != 1699999999000 {
		t.Fatalf("health/restarts/start time not carried through: %+v", c)
	}
}

func TestServerStateFromReportCarriesServiceErrors(t *testing.T) {
	state := serverStateFromReport("srv-1", testReport(), nil)

	b := state.Stacks[1]
	if b.Error == "" {
		t.Fatal("stack-level error must be carried through")
	}
	if len(b.ServiceErrors) != 1 || b.ServiceErrors[0].ServiceName != "api" {
		t.Fatalf("per-service errors must be carried through, got %v", b.ServiceErrors)
	}
	if len(b.Containers) != 0 {
		t.Fatalf("a stack with no containers must report none, got %d", len(b.Containers))
	}
}

func TestServerStateFromReportFiltersStacksTheCallerCannotView(t *testing.T) {
	// A stack-scoped binding must not leak the inventory of the other stacks on that server.
	state := serverStateFromReport("srv-1", testReport(), func(stackID string) bool {
		return stackID == "stack-a"
	})

	if len(state.Stacks) != 1 || state.Stacks[0].StackId != "stack-a" {
		t.Fatalf("expected only stack-a, got %v", state.Stacks)
	}
}

func TestServerStateFromReportHandlesEmptyReport(t *testing.T) {
	state := serverStateFromReport("srv-1", &orkestraV1.StatusReport{}, nil)

	if state.ServerId != "srv-1" || len(state.Stacks) != 0 || state.ReportedAt != 0 {
		t.Fatalf("empty report must convert to an empty state, got %+v", state)
	}
}
