package compose

import (
	"testing"
)

// TestLoadProjectIgnoresHostEnv pins #76: the agent process's environment must not feed compose
// interpolation. A ${VAR} that only exists in the host environment resolves to empty, while a
// value supplied by the assignment resolves normally.
func TestLoadProjectIgnoresHostEnv(t *testing.T) {
	t.Setenv("ORKESTRA_TEST_LEAK", "leaked-from-host")

	const yaml = `services:
  app:
    image: busybox:1.36
    environment:
      LEAK: "${ORKESTRA_TEST_LEAK}"
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

	if got := envValue(t, svc.Environment, "LEAK"); got != "" {
		t.Fatalf("host env leaked into interpolation: LEAK = %q, want empty", got)
	}
	if got := envValue(t, svc.Environment, "FROM_ASSIGNMENT"); got != "ok" {
		t.Fatalf("assignment value not interpolated: FROM_ASSIGNMENT = %q, want %q", got, "ok")
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
