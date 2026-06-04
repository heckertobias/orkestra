// Package compose implements the Converge Engine: parses Compose YAML and reconciles
// actual Docker container state toward the desired state.
package compose

import (
	"context"
	"fmt"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"
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

	// Use compose-go loader with an in-memory config.
	proj, err := loadFromBytes([]byte(composeYAML), stackID, env)
	if err != nil {
		return nil, fmt.Errorf("compose-go load: %w", err)
	}
	return proj, nil
}

// ValidateCompose parses the given YAML and returns a human-readable list of
// unsupported fields, or nil if the compose is valid for orkestra's MVP field matrix.
func ValidateCompose(_ context.Context, composeYAML string) []string {
	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(composeYAML), &raw); err != nil {
		return []string{fmt.Sprintf("YAML parse error: %v", err)}
	}
	var warnings []string
	// Unsupported top-level keys for MVP.
	unsupported := []string{"configs", "extensions"}
	services, _ := raw["services"].(map[string]interface{})
	for _, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []string{"deploy", "profiles", "links", "external_links", "scale"} {
			if _, has := svc[field]; has {
				warnings = append(warnings, fmt.Sprintf("field %q is not supported in MVP — it will be ignored", field))
			}
		}
	}
	for _, k := range unsupported {
		if _, has := raw[k]; has {
			warnings = append(warnings, fmt.Sprintf("top-level key %q is not supported and will be ignored", k))
		}
	}
	return warnings
}
