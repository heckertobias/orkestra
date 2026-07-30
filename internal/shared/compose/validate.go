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

// errorServiceFields are valid Compose fields whose presence makes the stack do the wrong thing if
// it is applied anyway, so they are a hard error. `volumes` and `extends` need value-level checks
// and are handled separately.
var errorServiceFields = map[string]string{
	"profiles": `"profiles" is not supported — a profiled service is loaded and then removed as an orphan (#54)`,
	"configs":  `"configs" is not supported — the config is never delivered to the container (#53)`,
}

// warnServiceFields are valid Compose fields orkestra silently ignores. The stack still runs, so a
// warning is enough — but the operator must know the field has no effect.
var warnServiceFields = map[string]string{
	"deploy":         `"deploy" is not supported and is ignored`,
	"links":          `"links" is not supported and is ignored`,
	"external_links": `"external_links" is not supported and is ignored`,
	"scale":          `"scale" is not supported and is ignored (#14)`,
	"secrets":        `compose "secrets" are not delivered — use orkestra Secrets instead (#22)`,
	"container_name": `"container_name" is ignored — orkestra names containers <project>-<service>-<id> (#56)`,
	"healthcheck":    `"healthcheck" is not applied — depends_on: condition: service_healthy will never be satisfied (#12)`,
	"logging":        `"logging" is not applied — the default json-file driver runs without a size limit (#13)`,
}

// errorTopLevelKeys are top-level compose keys that break reconciliation if present.
var errorTopLevelKeys = map[string]string{
	"include": `top-level "include" is not supported — the agent loads a single compose file, so the include fails at load time and the whole stack stops reconciling (#64)`,
}

// warnTopLevelKeys are top-level compose keys orkestra ignores.
var warnTopLevelKeys = map[string]string{
	"configs":    `top-level "configs" is not supported and is ignored (#53)`,
	"secrets":    `top-level "secrets" are not delivered — use orkestra Secrets instead (#22)`,
	"extensions": `top-level "extensions" is not supported and is ignored`,
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
		if msg, ok := errorTopLevelKeys[key.Value]; ok {
			diags = append(diags, Diagnostic{Severity: SeverityError, Message: msg, Line: key.Line})
			continue
		}
		if msg, ok := warnTopLevelKeys[key.Value]; ok {
			diags = append(diags, Diagnostic{Severity: SeverityWarning, Message: msg, Line: key.Line})
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
			if fieldName == "volumes" {
				diags = append(diags, checkServiceVolumes(svcName, svcNode.Content[j+1])...)
				continue
			}
			if fieldName == "extends" {
				diags = append(diags, checkServiceExtends(svcName, svcNode.Content[j+1])...)
				continue
			}
			if msg, ok := errorServiceFields[fieldName]; ok {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Message:  fmt.Sprintf("service %q: %s", svcName, msg),
					Line:     fieldKey.Line,
				})
			} else if msg, ok := warnServiceFields[fieldName]; ok {
				diags = append(diags, Diagnostic{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("service %q: %s", svcName, msg),
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

// checkServiceVolumes flags every volume entry that is not a bind mount. orkestra applies bind
// mounts only; a named or tmpfs volume that reaches the agent is silently dropped and, for a
// database, destroys data on the next recreate (#70). Named/tmpfs support is tracked in #11. This
// is a best-effort YAML-level check; the agent's converge engine is the authoritative backstop.
func checkServiceVolumes(svcName string, node *yaml.Node) []Diagnostic {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	var diags []Diagnostic
	for _, item := range node.Content {
		if ok, desc := isBindMount(item); !ok {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Message:  fmt.Sprintf("service %q: volume %q is not a bind mount — only bind mounts are supported (named/tmpfs volumes: #11)", svcName, desc),
				Line:     item.Line,
			})
		}
	}
	return diags
}

// checkServiceExtends allows in-file extends (compose-go resolves `extends: name` and
// `extends: {service: ...}` within the same file) but rejects `extends: {file: ...}`: the agent
// loads a single compose file with no sibling files, so a file-based extends fails at load time and
// the whole stack stops reconciling (#64).
func checkServiceExtends(svcName string, node *yaml.Node) []Diagnostic {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil // scalar short form `extends: name` refers to a service in the same file — supported
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "file" {
			return []Diagnostic{{
				Severity: SeverityError,
				Message:  fmt.Sprintf("service %q: \"extends.file\" is not supported — only in-file extends works (#64)", svcName),
				Line:     node.Content[i].Line,
			}}
		}
	}
	return nil
}

// isBindMount reports whether a single compose volume entry (short-string or long-mapping syntax)
// is a bind mount, and returns a short descriptor for the diagnostic. It mirrors compose-go's own
// rule: a source that is a host path is a bind mount, a bare name is a named volume. Unknown shapes
// are treated as bind mounts (no false positive) — the agent guard rejects anything it cannot apply.
func isBindMount(item *yaml.Node) (bool, string) {
	switch item.Kind {
	case yaml.ScalarNode:
		spec := item.Value
		// Short syntax: [SOURCE:]TARGET[:MODE]. No colon means a single TARGET — an anonymous volume.
		src, _, hasSource := strings.Cut(spec, ":")
		if !hasSource {
			return false, spec
		}
		return isHostPath(src), spec
	case yaml.MappingNode:
		var typ, source, target string
		for i := 0; i+1 < len(item.Content); i += 2 {
			switch item.Content[i].Value {
			case "type":
				typ = item.Content[i+1].Value
			case "source":
				source = item.Content[i+1].Value
			case "target":
				target = item.Content[i+1].Value
			}
		}
		desc := target
		if desc == "" {
			desc = source
		}
		if typ != "" {
			return typ == "bind", desc
		}
		return isHostPath(source), desc
	}
	return true, ""
}

// isHostPath reports whether a compose volume source refers to a host path (bind mount) rather than
// a named volume. Bind sources are absolute, relative, or home-anchored paths.
func isHostPath(s string) bool {
	switch {
	case s == "." || s == ".." || s == "~":
		return true
	case strings.HasPrefix(s, "/"),
		strings.HasPrefix(s, "./"),
		strings.HasPrefix(s, "../"),
		strings.HasPrefix(s, "~/"):
		return true
	default:
		return false
	}
}
