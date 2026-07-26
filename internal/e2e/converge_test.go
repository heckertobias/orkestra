//go:build integration

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/moby/moby/client"

	"github.com/heckertobias/orkestra/internal/agent/compose"
	"github.com/heckertobias/orkestra/internal/agent/dockerctl"
)

// TestConvergeDeploysContainer drives the real converge engine against a real Docker daemon:
// it deploys a one-service Compose project and asserts a managed container ends up running.
// Requires ORKESTRA_TEST_DOCKER to be set and a reachable Docker daemon (DOCKER_HOST / socket);
// otherwise it skips. Run in a dedicated CI job that provides Docker.
func TestConvergeDeploysContainer(t *testing.T) {
	if os.Getenv("ORKESTRA_TEST_DOCKER") == "" {
		t.Skip("set ORKESTRA_TEST_DOCKER=1 with a reachable Docker daemon to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dc, err := dockerctl.New()
	if err != nil {
		t.Fatalf("dockerctl.New: %v", err)
	}
	raw := dc.RawClient()
	if _, err := raw.Ping(ctx, client.PingOptions{}); err != nil {
		t.Skipf("no reachable Docker daemon: %v", err)
	}

	const stackID = "e2e-converge"
	const composeYAML = `services:
  sleeper:
    image: busybox:1.36
    command: ["sleep", "3600"]
`

	proj, err := compose.LoadProject(composeYAML, stackID, map[string]string{})
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	// Always clean up, even on failure.
	defer func() {
		cleanupCtx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		if err := compose.Remove(cleanupCtx, raw, stackID); err != nil {
			t.Logf("cleanup Remove: %v", err)
		}
	}()

	if err := compose.Converge(ctx, raw, stackID, proj); err != nil {
		t.Fatalf("Converge: %v", err)
	}

	// Assert a managed container for this stack is running.
	res, err := raw.ContainerList(ctx, client.ContainerListOptions{
		All: true,
		Filters: make(client.Filters).
			Add("label", "orkestra.managed=true").
			Add("label", "orkestra.stack-id="+stackID),
	})
	if err != nil {
		t.Fatalf("ContainerList: %v", err)
	}
	if len(res.Items) == 0 {
		t.Fatal("no managed container found after converge")
	}
	for _, c := range res.Items {
		if c.State != "running" {
			t.Fatalf("container %s state = %q, want running", c.ID[:12], c.State)
		}
	}
}

// TestListManagedStackIDsAndPrune verifies the full-state removal path behind unassign/delete
// (#72): a converged stack shows up in ListManagedStackIDs, and once it disappears from the
// desired state it is removed. Requires ORKESTRA_TEST_DOCKER and a reachable Docker daemon.
func TestListManagedStackIDsAndPrune(t *testing.T) {
	if os.Getenv("ORKESTRA_TEST_DOCKER") == "" {
		t.Skip("set ORKESTRA_TEST_DOCKER=1 with a reachable Docker daemon to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dc, err := dockerctl.New()
	if err != nil {
		t.Fatalf("dockerctl.New: %v", err)
	}
	raw := dc.RawClient()
	if _, err := raw.Ping(ctx, client.PingOptions{}); err != nil {
		t.Skipf("no reachable Docker daemon: %v", err)
	}

	const stackID = "e2e-prune"
	const composeYAML = `services:
  sleeper:
    image: busybox:1.36
    command: ["sleep", "3600"]
`

	proj, err := compose.LoadProject(composeYAML, stackID, map[string]string{})
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	// Always clean up, even on failure.
	defer func() {
		cleanupCtx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		if err := compose.Remove(cleanupCtx, raw, stackID); err != nil {
			t.Logf("cleanup Remove: %v", err)
		}
	}()

	if err := compose.Converge(ctx, raw, stackID, proj); err != nil {
		t.Fatalf("Converge: %v", err)
	}

	// The stack must be discoverable as a managed stack on the host.
	ids, err := compose.ListManagedStackIDs(ctx, raw)
	if err != nil {
		t.Fatalf("ListManagedStackIDs: %v", err)
	}
	if !containsID(ids, stackID) {
		t.Fatalf("ListManagedStackIDs = %v, want to contain %q", ids, stackID)
	}

	// Simulate the stack disappearing from the desired state: remove it and confirm it is gone.
	if err := compose.Remove(ctx, raw, stackID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	ids, err = compose.ListManagedStackIDs(ctx, raw)
	if err != nil {
		t.Fatalf("ListManagedStackIDs after remove: %v", err)
	}
	if containsID(ids, stackID) {
		t.Fatalf("ListManagedStackIDs = %v, want %q removed", ids, stackID)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
