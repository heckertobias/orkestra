//go:build integration

package e2e

import (
	"context"
	"os"
	"strings"
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

// TestConvergePartialFailure verifies #74: a single failing service must not abort the reconcile
// of the whole stack. One service has an unresolvable image; the healthy service must still come
// up, and Converge must return an error that names the broken service.
// Requires ORKESTRA_TEST_DOCKER and a reachable Docker daemon.
func TestConvergePartialFailure(t *testing.T) {
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

	const stackID = "e2e-partial"
	// "broken" sorts before "healthy", so without the fix the healthy service is never reached.
	const composeYAML = `services:
  broken:
    image: orkestra-nonexistent.invalid/does-not-exist:doesnotexist
  healthy:
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

	// Converge must report failure, and the error must name the broken service.
	err = compose.Converge(ctx, raw, stackID, proj)
	if err == nil {
		t.Fatal("Converge returned nil, want an error naming the failed service")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("Converge error = %q, want it to name the 'broken' service", err.Error())
	}

	// The healthy service must be running despite the broken one failing first.
	res, err := raw.ContainerList(ctx, client.ContainerListOptions{
		All: true,
		Filters: make(client.Filters).
			Add("label", "orkestra.managed=true").
			Add("label", "orkestra.stack-id="+stackID).
			Add("label", "orkestra.service=healthy"),
	})
	if err != nil {
		t.Fatalf("ContainerList: %v", err)
	}
	if len(res.Items) == 0 {
		t.Fatal("healthy service has no container — a failing sibling aborted the reconcile")
	}
	for _, c := range res.Items {
		if c.State != "running" {
			t.Fatalf("healthy container %s state = %q, want running", c.ID[:12], c.State)
		}
	}
}
