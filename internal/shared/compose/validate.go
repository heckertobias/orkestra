// Package compose provides shared compose-YAML utilities used by both Master and Agent.
package compose

import (
	"fmt"
	"strings"

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
	Line     int // 1-based line number in the source YAML; 0 = no specific position
}

// validServiceFields is the full set of field names recognised by the Compose spec.
// Anything not in this set (and not prefixed with "x-") is flagged as an error.
var validServiceFields = map[string]struct{}{
	"annotations": {}, "attach": {}, "blkio_config": {}, "build": {},
	"cap_add": {}, "cap_drop": {}, "cgroup": {}, "cgroup_parent": {},
	"command": {}, "configs": {}, "container_name": {},
	"cpu_count": {}, "cpu_percent": {}, "cpu_quota": {},
	"cpu_rt_period": {}, "cpu_rt_runtime": {}, "cpu_shares": {}, "cpus": {}, "cpuset": {},
	"credential_spec": {}, "depends_on": {}, "deploy": {}, "develop": {},
	"device_cgroup_rules": {}, "devices": {},
	"dns": {}, "dns_opt": {}, "dns_search": {}, "domainname": {},
	"entrypoint": {}, "env_file": {}, "environment": {}, "expose": {},
	"extends": {}, "external_links": {}, "extra_hosts": {}, "group_add": {},
	"healthcheck": {}, "hostname": {}, "image": {}, "init": {},
	"ipc": {}, "isolation": {}, "labels": {}, "links": {},
	"logging": {}, "mac_address": {},
	"mem_limit": {}, "mem_reservation": {}, "mem_swappiness": {}, "memswap_limit": {},
	"network_mode": {}, "networks": {},
	"oom_kill_disable": {}, "oom_score_adj": {},
	"pid": {}, "pids_limit": {}, "platform": {}, "ports": {},
	"privileged": {}, "profiles": {}, "pull_policy": {}, "read_only": {},
	"restart": {}, "runtime": {}, "scale": {}, "secrets": {},
	"security_opt": {}, "shm_size": {}, "stdin_open": {},
	"stop_grace_period": {}, "stop_signal": {}, "storage_opt": {},
	"sysctls": {}, "tmpfs": {}, "tty": {}, "ulimits": {},
	"user": {}, "userns_mode": {}, "volumes": {}, "volumes_from": {},
	"working_dir": {},
}

// unsupportedServiceFields is the subset of validServiceFields that orkestra
// recognises as valid Compose syntax but does not act on.
var unsupportedServiceFields = map[string]struct{}{
	"deploy":         {},
	"profiles":       {},
	"links":          {},
	"external_links": {},
	"scale":          {},
}

// unsupportedTopLevelKeys are top-level compose keys that orkestra ignores.
var unsupportedTopLevelKeys = map[string]struct{}{
	"configs":    {},
	"extensions": {},
}

// ValidateCompose parses the given YAML and returns diagnostics for YAML errors,
// unknown service fields (error), and unsupported-but-valid fields (warning).
// Returns nil when the compose is fully valid.
func ValidateCompose(composeYAML string) []Diagnostic {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(composeYAML), &root); err != nil {
		return []Diagnostic{{Severity: SeverityError, Message: fmt.Sprintf("YAML parse error: %v", err)}}
	}

	// Empty document
	if root.Kind == 0 || len(root.Content) == 0 {
		return nil
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}

	var diags []Diagnostic

	// Top-level key checks
	var servicesNode *yaml.Node
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i]
		if key.Value == "services" {
			servicesNode = doc.Content[i+1]
			continue
		}
		if strings.HasPrefix(key.Value, "x-") {
			continue
		}
		if key.Value == "name" {
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Message:  `top-level "name" is managed by orkestra and is ignored — the stack name is used`,
				Line:     key.Line,
			})
			continue
		}
		if _, unsup := unsupportedTopLevelKeys[key.Value]; unsup {
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("top-level key %q is not supported and will be ignored", key.Value),
				Line:     key.Line,
			})
		}
	}

	// Service field checks
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return diags
	}
	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		svcName := servicesNode.Content[i].Value
		svcNode := servicesNode.Content[i+1]
		if svcNode.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(svcNode.Content); j += 2 {
			fieldKey := svcNode.Content[j]
			fieldName := fieldKey.Value
			if strings.HasPrefix(fieldName, "x-") {
				continue
			}
			if _, unsup := unsupportedServiceFields[fieldName]; unsup {
				diags = append(diags, Diagnostic{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("service %q: field %q is not supported and will be ignored", svcName, fieldName),
					Line:     fieldKey.Line,
				})
			} else if _, valid := validServiceFields[fieldName]; !valid {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Message:  fmt.Sprintf("service %q: unknown field %q", svcName, fieldName),
					Line:     fieldKey.Line,
				})
			}
		}
	}

	return diags
}
