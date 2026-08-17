// Package compose implements the Converge Engine: parses Compose YAML and reconciles
// actual Docker container state toward the desired state.
package compose

import (
	"context"
	"fmt"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"

	sharedcompose "github.com/heckertobias/orkestra/internal/shared/compose"
)

// LoadProject parses a Compose YAML string into a compose-go Project.
// envVars are merged over the YAML's own environment declarations.
func LoadProject(composeYAML string, stackID string, envVars map[string]string) (*composetypes.Project, error) {
	// compose-go's loader requires a project directory and config files.
	// We parse the YAML directly via yaml.v3 → map, then build a minimal Project.
	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(composeYAML), &raw); err != nil {
		return nil, fmt.Errorf("parse compose YAML: %w", err)
	}

	// Build env mapping for substitution.
	env := make(map[string]string)
	for k, v := range envVars {
		env[k] = v
	}

	// Refuse to converge a project whose `${VAR}` references would interpolate to an empty string
	// (#81). compose-go substitutes silently, so `image: ${REGISTRY}/app:${TAG}` would become
	// `/app:` and fail later with a Docker error naming neither variable. The Master rejects this at
	// assign time; this is the backstop for desired state that predates that check or reaches the
	// agent another way. The error propagates up through the reconciler and is reported per stack.
	if missing := sharedcompose.MissingVars(composeYAML, env); len(missing) > 0 {
		return nil, fmt.Errorf("unresolved compose variables (no value and no default): %s",
			strings.Join(sharedcompose.VarNames(missing), ", "))
	}

	// Use compose-go loader with an in-memory config.
	proj, err := loadFromBytes([]byte(composeYAML), stackID, env)
	if err != nil {
		return nil, fmt.Errorf("compose-go load: %w", err)
	}
	return proj, nil
}

// ValidateCompose parses the given YAML and returns a human-readable list of
// unsupported fields, or nil if the compose is valid for orkestra's MVP field matrix.
// Delegates to the shared implementation.
func ValidateCompose(_ context.Context, composeYAML string) []string {
	diags := sharedcompose.ValidateCompose(composeYAML)
	if len(diags) == 0 {
		return nil
	}
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.Message
	}
	return msgs
}
