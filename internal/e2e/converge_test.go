//go:build integration

package e2e

import (
	"context"
	"io"
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

// TestConvergeFailedPullKeepsRunning verifies #73: a failed pull must not tear down the running
// container. A service is deployed on a good image, then repointed at an unresolvable image with
// pull_policy: always; the pull fails in phase 1, before any removal, so the original container
// keeps running. Requires ORKESTRA_TEST_DOCKER and a reachable Docker daemon.
func TestConvergeFailedPullKeepsRunning(t *testing.T) {
	ctx, raw := dockerForTest(t)

	const stackID = "e2e-failpull"
	goodYAML := `services:
  app:
    image: busybox:1.36
    command: ["sleep", "3600"]
`
	badYAML := `services:
  app:
    image: orkestra-nonexistent.invalid/does-not-exist:doesnotexist
    pull_policy: always
    command: ["sleep", "3600"]
`
	defer cleanupStack(raw, stackID)

	// Deploy the good version and capture the running container's id.
	good, err := compose.LoadProject(goodYAML, stackID, map[string]string{})
	if err != nil {
		t.Fatalf("LoadProject good: %v", err)
	}
	if err := compose.Converge(ctx, raw, stackID, good); err != nil {
		t.Fatalf("Converge good: %v", err)
	}
	before := stackContainerIDs(ctx, t, raw, stackID)
	if len(before) != 1 {
		t.Fatalf("expected 1 container after initial deploy, got %d", len(before))
	}

	// Converge the bad version: the pull must fail and the original container must survive.
	bad, err := compose.LoadProject(badYAML, stackID, map[string]string{})
	if err != nil {
		t.Fatalf("LoadProject bad: %v", err)
	}
	if err := compose.Converge(ctx, raw, stackID, bad); err == nil {
		t.Fatal("Converge bad returned nil, want a pull error")
	}
	after := stackContainerIDs(ctx, t, raw, stackID)
	if len(after) != 1 || after[0] != before[0] {
		t.Fatalf("original container not preserved across failed pull: before=%v after=%v", before, after)
	}
}

// TestConvergeMovedTagRecreates verifies #73: when a tag is repointed to different image content,
// the resolved image ID changes the spec-hash and the container is recreated — a plain tag string
// never would. Requires ORKESTRA_TEST_DOCKER and a reachable Docker daemon.
func TestConvergeMovedTagRecreates(t *testing.T) {
	ctx, raw := dockerForTest(t)

	// Two distinct images to move a shared tag between.
	for _, ref := range []string{"busybox:1.36", "busybox:1.37"} {
		if err := pullImage(ctx, raw, ref); err != nil {
			t.Skipf("cannot pull %s: %v", ref, err)
		}
	}
	const movedTag = "orktest-moved:latest"
	tag := func(src string) {
		if _, err := raw.ImageTag(ctx, client.ImageTagOptions{Source: src, Target: movedTag}); err != nil {
			t.Fatalf("ImageTag %s -> %s: %v", src, movedTag, err)
		}
	}

	const stackID = "e2e-movedtag"
	composeYAML := `services:
  app:
    image: ` + movedTag + `
    command: ["sleep", "3600"]
`
	defer cleanupStack(raw, stackID)

	// Point the tag at image A and deploy; pull_policy defaults to missing so the local tag is used.
	tag("busybox:1.36")
	proj, err := compose.LoadProject(composeYAML, stackID, map[string]string{})
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if err := compose.Converge(ctx, raw, stackID, proj); err != nil {
		t.Fatalf("Converge A: %v", err)
	}
	before := stackContainerIDs(ctx, t, raw, stackID)
	if len(before) != 1 {
		t.Fatalf("expected 1 container after deploy, got %d", len(before))
	}

	// Repoint the same tag at image B and converge again: the resolved ID changed → recreate.
	tag("busybox:1.37")
	if err := compose.Converge(ctx, raw, stackID, proj); err != nil {
		t.Fatalf("Converge B: %v", err)
	}
	after := stackContainerIDs(ctx, t, raw, stackID)
	if len(after) != 1 {
		t.Fatalf("expected 1 container after recreate, got %d", len(after))
	}
	if after[0] == before[0] {
		t.Fatalf("container was not recreated after the tag moved (still %s)", before[0][:12])
	}
}

// dockerForTest skips unless a Docker daemon is available, and returns a ready context + client.
func dockerForTest(t *testing.T) (context.Context, *client.Client) {
	t.Helper()
	if os.Getenv("ORKESTRA_TEST_DOCKER") == "" {
		t.Skip("set ORKESTRA_TEST_DOCKER=1 with a reachable Docker daemon to run this test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	dc, err := dockerctl.New()
	if err != nil {
		t.Fatalf("dockerctl.New: %v", err)
	}
	raw := dc.RawClient()
	if _, err := raw.Ping(ctx, client.PingOptions{}); err != nil {
		t.Skipf("no reachable Docker daemon: %v", err)
	}
	return ctx, raw
}

func pullImage(ctx context.Context, raw *client.Client, ref string) error {
	rc, err := raw.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	_, err = io.Copy(io.Discard, rc)
	return err
}

func stackContainerIDs(ctx context.Context, t *testing.T, raw *client.Client, stackID string) []string {
	t.Helper()
	res, err := raw.ContainerList(ctx, client.ContainerListOptions{
		All: true,
		Filters: make(client.Filters).
			Add("label", "orkestra.managed=true").
			Add("label", "orkestra.stack-id="+stackID),
	})
	if err != nil {
		t.Fatalf("ContainerList: %v", err)
	}
	ids := make([]string, 0, len(res.Items))
	for _, c := range res.Items {
		ids = append(ids, c.ID)
	}
	return ids
}

func cleanupStack(raw *client.Client, stackID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = compose.Remove(ctx, raw, stackID)
}
