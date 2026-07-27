package compose

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func TestSpecHashIncludesImageID(t *testing.T) {
	svc := composetypes.ServiceConfig{Image: "app:latest"}

	// A repointed tag (same string, new resolved ID) must change identity so the container is
	// recreated — the core of #73.
	if a, b := specHash(svc, "sha256:aaa"), specHash(svc, "sha256:bbb"); a == b {
		t.Fatalf("spec-hash must change when the resolved image ID changes (got %q for both)", a)
	}
	// And it must stay stable for identical inputs, or every reconcile would recreate.
	if a, b := specHash(svc, "sha256:aaa"), specHash(svc, "sha256:aaa"); a != b {
		t.Fatalf("spec-hash must be stable for identical inputs: %q != %q", a, b)
	}
}

func TestShouldPull(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		present bool
		want    bool
	}{
		{"default pulls when missing", "", false, true},
		{"default skips when present", "", true, false},
		{"missing pulls when missing", composetypes.PullPolicyMissing, false, true},
		{"missing skips when present", composetypes.PullPolicyMissing, true, false},
		{"if_not_present pulls when missing", composetypes.PullPolicyIfNotPresent, false, true},
		{"if_not_present skips when present", composetypes.PullPolicyIfNotPresent, true, false},
		{"always pulls when present", composetypes.PullPolicyAlways, true, true},
		{"always pulls when missing", composetypes.PullPolicyAlways, false, true},
		{"never skips when missing", composetypes.PullPolicyNever, false, false},
		{"never skips when present", composetypes.PullPolicyNever, true, false},
		{"build skips when missing", composetypes.PullPolicyBuild, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPull(tc.policy, tc.present); got != tc.want {
				t.Errorf("shouldPull(%q, %v) = %v, want %v", tc.policy, tc.present, got, tc.want)
			}
		})
	}
}
