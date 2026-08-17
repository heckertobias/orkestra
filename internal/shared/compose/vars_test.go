package compose

import (
	"reflect"
	"testing"
)

func TestExtractVarRefs(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []VarRef
	}{
		{
			name: "plain reference",
			yaml: "services:\n  web:\n    image: ${IMAGE}\n",
			want: []VarRef{{Name: "IMAGE", Line: 3}},
		},
		{
			name: "two references on one line",
			yaml: "services:\n  web:\n    image: ${REGISTRY}/app:${TAG}\n",
			want: []VarRef{{Name: "REGISTRY", Line: 3}, {Name: "TAG", Line: 3}},
		},
		{
			name: "colon-dash default",
			yaml: "services:\n  web:\n    image: nginx:${TAG:-latest}\n",
			want: []VarRef{{Name: "TAG", Line: 3, HasDefault: true}},
		},
		{
			name: "bare dash default",
			yaml: "services:\n  web:\n    image: nginx:${TAG-latest}\n",
			want: []VarRef{{Name: "TAG", Line: 3, HasDefault: true}},
		},
		{
			name: "empty default is still a default",
			yaml: "services:\n  web:\n    image: nginx${SUFFIX:-}\n",
			want: []VarRef{{Name: "SUFFIX", Line: 3, HasDefault: true}},
		},
		{
			name: "alternate form counts as resolved",
			yaml: "services:\n  web:\n    image: nginx${DEBUG:+-debug}\n",
			want: []VarRef{{Name: "DEBUG", Line: 3, HasDefault: true}},
		},
		{
			name: "required form",
			yaml: "services:\n  web:\n    image: ${IMAGE:?must be set}\n",
			want: []VarRef{{Name: "IMAGE", Line: 3, Required: true}},
		},
		{
			name: "required form without colon",
			yaml: "services:\n  web:\n    image: ${IMAGE?must be set}\n",
			want: []VarRef{{Name: "IMAGE", Line: 3, Required: true}},
		},
		{
			name: "bare reference",
			yaml: "services:\n  web:\n    image: $IMAGE\n",
			want: []VarRef{{Name: "IMAGE", Line: 3}},
		},
		{
			name: "double dollar is an escape, not a reference",
			yaml: "services:\n  web:\n    command: echo $$HOME\n",
			want: nil,
		},
		{
			name: "escape followed by a real reference",
			yaml: "services:\n  web:\n    command: echo $$HOME ${REAL}\n",
			want: []VarRef{{Name: "REAL", Line: 3}},
		},
		{
			name: "reference in a comment is ignored",
			yaml: "services:\n  web:\n    # image: ${COMMENTED}\n    image: nginx\n",
			want: nil,
		},
		{
			name: "reference in a mapping key",
			yaml: "services:\n  web:\n    labels:\n      ${LABEL_KEY}: value\n",
			want: []VarRef{{Name: "LABEL_KEY", Line: 4}},
		},
		{
			name: "reference in a list item",
			yaml: "services:\n  web:\n    ports:\n      - \"${HOST_PORT}:80\"\n",
			want: []VarRef{{Name: "HOST_PORT", Line: 4}},
		},
		{
			name: "unterminated brace is not a reference",
			yaml: "services:\n  web:\n    image: ${BROKEN\n",
			want: nil,
		},
		{
			name: "invalid YAML yields nothing",
			yaml: "services:\n  web:\n   image: ${A}\n  \tbad: indent\n",
			want: nil,
		},
		{
			name: "empty document",
			yaml: "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractVarRefs(tt.yaml)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractVarRefs()\n got = %+v\nwant = %+v", got, tt.want)
			}
		})
	}
}

func TestMissingVars(t *testing.T) {
	const yaml = `services:
  web:
    image: ${REGISTRY}/app:${TAG}
    environment:
      OPTIONAL: ${OPTIONAL:-fallback}
      HARD: ${HARD:?must be set}
`

	tests := []struct {
		name   string
		values map[string]string
		want   []string
	}{
		{
			name:   "nothing supplied",
			values: nil,
			want:   []string{"REGISTRY", "TAG"},
		},
		{
			name:   "one supplied",
			values: map[string]string{"REGISTRY": "ghcr.io/acme"},
			want:   []string{"TAG"},
		},
		{
			name:   "all supplied",
			values: map[string]string{"REGISTRY": "ghcr.io/acme", "TAG": "v1"},
			want:   nil,
		},
		{
			name:   "empty value counts as unset",
			values: map[string]string{"REGISTRY": "ghcr.io/acme", "TAG": ""},
			want:   []string{"TAG"},
		},
		{
			name:   "whitespace-only value counts as unset",
			values: map[string]string{"REGISTRY": "ghcr.io/acme", "TAG": "   "},
			want:   []string{"TAG"},
		},
		{
			name:   "a supplied value for a defaulted var changes nothing",
			values: map[string]string{"REGISTRY": "r", "TAG": "t", "OPTIONAL": ""},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VarNames(MissingVars(yaml, tt.values))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MissingVars() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMissingVars_ReportsFirstOccurrence pins that a variable referenced several times is reported
// once, at the line it first appears on — the message points at something the author can act on.
func TestMissingVars_ReportsFirstOccurrence(t *testing.T) {
	const yaml = `services:
  web:
    image: nginx:${TAG}
  api:
    image: api:${TAG}
`
	missing := MissingVars(yaml, nil)
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing var, got %+v", missing)
	}
	if missing[0].Name != "TAG" || missing[0].Line != 3 {
		t.Errorf("got %+v, want TAG on line 3", missing[0])
	}
}
