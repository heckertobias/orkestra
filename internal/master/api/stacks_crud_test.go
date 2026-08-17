package api

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
)

// TestValidateEnvValuesOrError pins the assign-time half of #81: an assignment whose compose
// references variables with no value and no default is refused, and the error names them.
func TestValidateEnvValuesOrError(t *testing.T) {
	const yaml = `services:
  web:
    image: ${REGISTRY}/app:${TAG}
    environment:
      OPTIONAL: ${OPTIONAL:-fallback}
`

	tests := []struct {
		name       string
		values     map[string]string
		wantErr    bool
		wantNamed  []string
		wantAbsent []string
	}{
		{
			name:      "nothing supplied",
			values:    nil,
			wantErr:   true,
			wantNamed: []string{"REGISTRY", "TAG"},
			// A defaulted reference resolves on its own and must never be demanded.
			wantAbsent: []string{"OPTIONAL"},
		},
		{
			name:      "empty value counts as unset",
			values:    map[string]string{"REGISTRY": "ghcr.io/acme", "TAG": ""},
			wantErr:   true,
			wantNamed: []string{"TAG"},
			// REGISTRY is satisfied and must not be dragged into the message.
			wantAbsent: []string{"REGISTRY"},
		},
		{
			name:    "all supplied",
			values:  map[string]string{"REGISTRY": "ghcr.io/acme", "TAG": "v1"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEnvValuesOrError(yaml, tt.values)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
				t.Errorf("code = %v, want %v", got, connect.CodeInvalidArgument)
			}
			for _, name := range tt.wantNamed {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error should name %q, got: %v", name, err)
				}
			}
			for _, name := range tt.wantAbsent {
				if strings.Contains(err.Error(), name) {
					t.Errorf("error should not name %q, got: %v", name, err)
				}
			}
		})
	}
}

// TestValidateEnvValuesOrError_EmptyCompose guards the stack-without-a-version case: there is
// nothing to interpolate, so there is nothing to demand.
func TestValidateEnvValuesOrError_EmptyCompose(t *testing.T) {
	if err := validateEnvValuesOrError("", nil); err != nil {
		t.Fatalf("expected no error for empty compose, got %v", err)
	}
}
