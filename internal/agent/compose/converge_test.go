package compose

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/container"
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

func TestBuildBinds(t *testing.T) {
	t.Run("bind mounts pass through", func(t *testing.T) {
		vols := []composetypes.ServiceVolumeConfig{
			{Type: "bind", Source: "/host/data", Target: "/data"},
			{Type: "bind", Source: "/host/ro", Target: "/ro", ReadOnly: true},
		}
		got, err := buildBinds(vols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"/host/data:/data", "/host/ro:/ro:ro"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("bind[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("named volume is rejected", func(t *testing.T) {
		vols := []composetypes.ServiceVolumeConfig{{Type: "volume", Source: "dbdata", Target: "/data"}}
		if _, err := buildBinds(vols); err == nil {
			t.Fatal("expected an error for a named volume, got nil")
		}
	})
	t.Run("tmpfs is rejected", func(t *testing.T) {
		vols := []composetypes.ServiceVolumeConfig{{Type: "tmpfs", Target: "/cache"}}
		if _, err := buildBinds(vols); err == nil {
			t.Fatal("expected an error for a tmpfs mount, got nil")
		}
	})
	t.Run("no volumes is fine", func(t *testing.T) {
		got, err := buildBinds(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected no binds, got %v", got)
		}
	})
}

func TestShouldPull(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		present bool
		want    bool
		wantErr bool
	}{
		{"default pulls when missing", "", false, true, false},
		{"default skips when present", "", true, false, false},
		{"missing pulls when missing", composetypes.PullPolicyMissing, false, true, false},
		{"missing skips when present", composetypes.PullPolicyMissing, true, false, false},
		{"if_not_present pulls when missing", composetypes.PullPolicyIfNotPresent, false, true, false},
		{"if_not_present skips when present", composetypes.PullPolicyIfNotPresent, true, false, false},
		{"always pulls when present", composetypes.PullPolicyAlways, true, true, false},
		{"always pulls when missing", composetypes.PullPolicyAlways, false, true, false},
		{"never skips when missing", composetypes.PullPolicyNever, false, false, false},
		{"never skips when present", composetypes.PullPolicyNever, true, false, false},
		{"build skips when missing", composetypes.PullPolicyBuild, false, false, false},
		{"refresh is rejected", "refresh", false, false, true},
		{"daily is rejected", "daily", false, false, true},
		{"weekly is rejected", "weekly", true, false, true},
		{"every_* is rejected", "every_5m", false, false, true},
		{"unknown value is rejected", "sometimes", false, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := shouldPull(tc.policy, tc.present)
			if (err != nil) != tc.wantErr {
				t.Fatalf("shouldPull(%q, %v) error = %v, wantErr %v", tc.policy, tc.present, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("shouldPull(%q, %v) = %v, want %v", tc.policy, tc.present, got, tc.want)
			}
		})
	}
}

func TestToRestartPolicy(t *testing.T) {
	tests := []struct {
		name     string
		policy   string
		wantName container.RestartPolicyMode
		wantMax  int
		wantErr  bool
	}{
		{"empty means no", "", container.RestartPolicyDisabled, 0, false},
		{"explicit no", "no", container.RestartPolicyDisabled, 0, false},
		{"always", "always", container.RestartPolicyAlways, 0, false},
		{"unless-stopped", "unless-stopped", container.RestartPolicyUnlessStopped, 0, false},
		{"on-failure without count", "on-failure", container.RestartPolicyOnFailure, 0, false},
		{"on-failure with count", "on-failure:5", container.RestartPolicyOnFailure, 5, false},
		{"on-failure zero count", "on-failure:0", container.RestartPolicyOnFailure, 0, false},
		{"on-failure non-integer is rejected", "on-failure:abc", "", 0, true},
		{"on-failure negative is rejected", "on-failure:-1", "", 0, true},
		{"typo is rejected", "alwasy", "", 0, true},
		{"unknown value is rejected", "restart-always", "", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toRestartPolicy(tc.policy)
			if (err != nil) != tc.wantErr {
				t.Fatalf("toRestartPolicy(%q) error = %v, wantErr %v", tc.policy, err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got.Name != tc.wantName || got.MaximumRetryCount != tc.wantMax {
				t.Errorf("toRestartPolicy(%q) = {Name:%q, Max:%d}, want {Name:%q, Max:%d}",
					tc.policy, got.Name, got.MaximumRetryCount, tc.wantName, tc.wantMax)
			}
		})
	}
}
