package status

import (
	"context"
	"testing"

	"github.com/heckertobias/orkestra/internal/agent/compose"
)

// buildStacks is exercised with a nil Docker client: the inspect enrichment is skipped, which
// keeps every assertion here about grouping and merging rather than about Docker.

func TestBuildStacksGroupsContainersByStack(t *testing.T) {
	containers := []compose.ManagedContainer{
		{ID: "c2", StackID: "s1", Service: "web", StackVersion: "v1", State: "running", Health: "healthy"},
		{ID: "c1", StackID: "s1", Service: "db", StackVersion: "v1", State: "running"},
		{ID: "c3", StackID: "s2", Service: "api", StackVersion: "v7", State: "exited"},
	}

	stacks := buildStacks(context.Background(), nil, containers, nil)

	if len(stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(stacks))
	}
	if stacks[0].StackId != "s1" || stacks[1].StackId != "s2" {
		t.Fatalf("stacks must be reported in a stable order, got %q and %q", stacks[0].StackId, stacks[1].StackId)
	}
	// Containers are sorted by service so the UI table does not reshuffle between reports.
	if got := []string{stacks[0].Containers[0].ServiceName, stacks[0].Containers[1].ServiceName}; got[0] != "db" || got[1] != "web" {
		t.Fatalf("containers must be sorted by service, got %v", got)
	}
	if stacks[0].RunningVersion != "v1" {
		t.Fatalf("running version = %q, want v1", stacks[0].RunningVersion)
	}
	if stacks[0].Containers[1].Health != "healthy" {
		t.Fatalf("health must be carried through, got %q", stacks[0].Containers[1].Health)
	}
}

func TestBuildStacksReportsNoVersionWhenContainersDisagree(t *testing.T) {
	// A half-finished rollout has no single running version; reporting either one would claim
	// a state the host is not in.
	containers := []compose.ManagedContainer{
		{ID: "c1", StackID: "s1", Service: "db", StackVersion: "v1"},
		{ID: "c2", StackID: "s1", Service: "web", StackVersion: "v2"},
	}

	stacks := buildStacks(context.Background(), nil, containers, nil)

	if stacks[0].RunningVersion != "" {
		t.Fatalf("mixed versions must report no running version, got %q", stacks[0].RunningVersion)
	}
}

func TestBuildStacksReportsStackWithoutContainers(t *testing.T) {
	// A stack that failed before any container was created leaves no trace on the host, so the
	// recorded outcome is the only way its failure reaches the Master.
	outcomes := map[string]StackOutcome{
		"s1": {Version: "v1", Err: "converge s1: 1 service(s) failed", ServiceErrors: map[string]string{"web": "pull failed"}},
	}

	stacks := buildStacks(context.Background(), nil, nil, outcomes)

	if len(stacks) != 1 {
		t.Fatalf("expected the failed stack to be reported, got %d stacks", len(stacks))
	}
	if stacks[0].Error == "" {
		t.Fatal("stack-level error must be reported")
	}
	if len(stacks[0].ServiceErrors) != 1 || stacks[0].ServiceErrors[0].ServiceName != "web" {
		t.Fatalf("per-service error must be reported, got %v", stacks[0].ServiceErrors)
	}
	if stacks[0].ServiceErrors[0].Error != "pull failed" {
		t.Fatalf("service error = %q, want %q", stacks[0].ServiceErrors[0].Error, "pull failed")
	}
}

func TestBuildStacksSkipsUnattributableContainers(t *testing.T) {
	// Managed but without a stack label: nothing useful can be said about it, and it must not
	// create a stack with an empty ID.
	containers := []compose.ManagedContainer{{ID: "c1", Service: "web"}}

	if stacks := buildStacks(context.Background(), nil, containers, nil); len(stacks) != 0 {
		t.Fatalf("expected no stacks, got %d", len(stacks))
	}
}

func TestStorePruneDropsUndesiredStacks(t *testing.T) {
	s := NewStore()
	s.Set("s1", StackOutcome{Version: "v1"})
	s.Set("s2", StackOutcome{Version: "v1", Err: "boom"})

	s.Prune(map[string]struct{}{"s1": {}})

	got := s.snapshot()
	if _, ok := got["s2"]; ok {
		t.Fatal("outcome of an unassigned stack must not keep being reported")
	}
	if _, ok := got["s1"]; !ok {
		t.Fatal("outcome of a desired stack must be kept")
	}
}

func TestReportWithoutDockerStillCarriesOutcomes(t *testing.T) {
	// Docker being unavailable must not silence the heartbeat: the Master uses the report's
	// arrival to keep the server online.
	s := NewStore()
	s.Set("s1", StackOutcome{Err: "docker unavailable"})

	report := s.Report(context.Background(), nil)

	if report.ReportedAtMs == 0 {
		t.Fatal("report must carry a timestamp")
	}
	if len(report.Stacks) != 1 || report.Stacks[0].Error != "docker unavailable" {
		t.Fatalf("expected the recorded outcome to be reported, got %v", report.Stacks)
	}
}
