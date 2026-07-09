package compose

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

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
