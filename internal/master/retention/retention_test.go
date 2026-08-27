package retention

import (
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		dbDays  int
		envDays int
		want    time.Duration
	}{
		{
			name:    "unset inherits the startup default",
			dbDays:  -1,
			envDays: 30,
			want:    30 * 24 * time.Hour,
		},
		{
			name:    "an explicit window beats the startup default",
			dbDays:  7,
			envDays: 30,
			want:    7 * 24 * time.Hour,
		},
		{
			// The whole point of the opt-in model: an admin who sets 0 in the UI must be
			// able to turn deletion back off, even when the env var asks for pruning.
			name:    "zero keeps rows forever and overrides the startup default",
			dbDays:  0,
			envDays: 30,
			want:    KeepForever,
		},
		{
			name:    "unset with a keep-forever default keeps rows forever",
			dbDays:  -1,
			envDays: 0,
			want:    KeepForever,
		},
		{
			// A negative startup default is nonsense input; it must not become a rule that
			// deletes rows with a cutoff in the future.
			name:    "a negative startup default never deletes",
			dbDays:  -1,
			envDays: -5,
			want:    KeepForever,
		},
		{
			name:    "values below -1 are still treated as unset",
			dbDays:  -99,
			envDays: 14,
			want:    14 * 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.dbDays, tt.envDays); got != tt.want {
				t.Fatalf("Resolve(%d, %d) = %v, want %v", tt.dbDays, tt.envDays, got, tt.want)
			}
		})
	}
}
