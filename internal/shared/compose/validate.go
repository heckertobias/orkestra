// Package compose provides shared compose-YAML utilities used by both Master and Agent.
package compose

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Severity classifies a diagnostic message.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is a single validation finding (parse error or unsupported-field warning).
type Diagnostic struct {
	Severity Severity
	Message  string
}

// ValidateCompose parses the given YAML and returns diagnostics for YAML errors and
// unsupported Compose fields.  Returns nil when the compose is fully valid.
func ValidateCompose(composeYAML string) []Diagnostic {
	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(composeYAML), &raw); err != nil {
		return []Diagnostic{{Severity: SeverityError, Message: fmt.Sprintf("YAML parse error: %v", err)}}
	}

	var diags []Diagnostic

	services, _ := raw["services"].(map[string]interface{})
	for svcName, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []string{"deploy", "profiles", "links", "external_links", "scale"} {
			if _, has := svc[field]; has {
				diags = append(diags, Diagnostic{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("service %q: field %q is not supported and will be ignored", svcName, field),
				})
			}
		}
	}

	for _, k := range []string{"configs", "extensions"} {
		if _, has := raw[k]; has {
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("top-level key %q is not supported and will be ignored", k),
			})
		}
	}

	return diags
}
