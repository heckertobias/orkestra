//go:build integration

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"

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
	if _, err := raw.Ping(ctx); err != nil {
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
	containers, err := raw.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "orkestra.managed=true"),
			filters.Arg("label", "orkestra.stack-id="+stackID),
		),
	})
	if err != nil {
		t.Fatalf("ContainerList: %v", err)
	}
	if len(containers) == 0 {
		t.Fatal("no managed container found after converge")
	}
	for _, c := range containers {
		if c.State != "running" {
			t.Fatalf("container %s state = %q, want running", c.ID[:12], c.State)
		}
	}
}
