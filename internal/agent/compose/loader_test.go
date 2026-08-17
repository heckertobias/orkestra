package compose

import (
	"strings"
	"testing"
)

// TestLoadProjectIgnoresHostEnv pins #76: the agent process's environment must not feed compose
// interpolation. Since #81 a variable with no value and no default is an error rather than an empty
// string, so the host-only variable now surfaces as a *rejection naming it* — which proves the same
// invariant more sharply than the old empty-string assertion did.
func TestLoadProjectIgnoresHostEnv(t *testing.T) {
	t.Setenv("ORKESTRA_TEST_LEAK", "leaked-from-host")

	const yaml = `services:
  app:
    image: busybox:1.36
    environment:
      LEAK: "${ORKESTRA_TEST_LEAK}"
`
	_, err := LoadProject(yaml, "stack-under-test", nil)
	if err == nil {
		t.Fatal("host env leaked into interpolation: LoadProject succeeded, want an unresolved-variable error")
	}
	if !strings.Contains(err.Error(), "ORKESTRA_TEST_LEAK") {
		t.Fatalf("error should name the unresolved variable, got: %v", err)
	}
}

// TestLoadProjectUsesAssignmentValues is the other half of #76: a value supplied by the assignment
// resolves normally.
func TestLoadProjectUsesAssignmentValues(t *testing.T) {
	const yaml = `services:
  app:
    image: busybox:1.36
    environment:
      FROM_ASSIGNMENT: "${ASSIGNED}"
`
	proj, err := LoadProject(yaml, "stack-under-test", map[string]string{"ASSIGNED": "ok"})
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	svc, ok := proj.Services["app"]
	if !ok {
		t.Fatal("service app not found in project")
	}
	if got := envValue(t, svc.Environment, "FROM_ASSIGNMENT"); got != "ok" {
		t.Fatalf("assignment value not interpolated: FROM_ASSIGNMENT = %q, want %q", got, "ok")
	}
}

// TestLoadProjectRejectsUnresolvedVariables pins #81 for the agent backstop: a reference with no
// value and no default is refused, while a defaulted one loads.
func TestLoadProjectRejectsUnresolvedVariables(t *testing.T) {
	const yaml = `services:
  app:
    image: ${REGISTRY}/app:${TAG:-latest}
`
	_, err := LoadProject(yaml, "stack-under-test", nil)
	if err == nil {
		t.Fatal("expected an error for the unresolved ${REGISTRY}")
	}
	if !strings.Contains(err.Error(), "REGISTRY") {
		t.Errorf("error should name REGISTRY, got: %v", err)
	}
	if strings.Contains(err.Error(), "TAG") {
		t.Errorf("TAG has a default and must not be reported, got: %v", err)
	}

	if _, err := LoadProject(yaml, "stack-under-test", map[string]string{"REGISTRY": "ghcr.io/acme"}); err != nil {
		t.Fatalf("LoadProject with REGISTRY supplied: %v", err)
	}
}

// envValue reads a resolved environment entry, treating an unset/nil value as empty.
func envValue(t *testing.T, env map[string]*string, key string) string {
	t.Helper()
	v, ok := env[key]
	if !ok || v == nil {
		return ""
	}
	return *v
}
