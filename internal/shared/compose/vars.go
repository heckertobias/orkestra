package compose

import (
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// VarRef is a single interpolation reference (`${VAR}` or `$VAR`) found in a compose YAML.
type VarRef struct {
	Name       string
	Line       int  // 1-based line in the source YAML; 0 = no specific position
	HasDefault bool // ${VAR:-x} / ${VAR-x} — resolves even without a value
	Required   bool // ${VAR:?msg} / ${VAR?msg} — compose-go fails on its own
}

// varPattern matches the braced form `${NAME<modifier>}` and captures the name plus the modifier
// character(s) that follow it. The bare form `$NAME` is matched separately below. Both mirror the
// regexes in web/src/lib/env.ts so the editor and the backend agree on what counts as a reference.
// Both are anchored: the scanner applies them at a known `$`, never as a free search.
var varPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)([^}]*)\}`)

// bareVarPattern matches `$NAME` without braces. `$$NAME` is a compose escape for a literal `$` and
// must not be treated as a reference, which is why the scanner below consumes `$$` explicitly
// instead of relying on a lookbehind (Go's regexp has none).
var bareVarPattern = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)`)

// ExtractVarRefs returns every interpolation reference in the compose YAML, in document order,
// with the line it appears on.
//
// It walks the parsed YAML node tree and inspects scalar nodes only. A regex over the raw text
// would also match inside `#` comments, and a commented-out `${VAR}` must not block a deploy.
// For multi-line block scalars the reported line is where the scalar starts.
//
// The same variable may appear more than once — once per reference. Callers that want unique names
// should deduplicate; MissingVars does.
func ExtractVarRefs(composeYAML string) []VarRef {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(composeYAML), &root); err != nil {
		// A YAML parse error is ValidateCompose's business; there is nothing to extract here.
		return nil
	}
	var refs []VarRef
	walkScalars(&root, func(n *yaml.Node) {
		refs = append(refs, refsInScalar(n.Value, n.Line)...)
	})
	return refs
}

// MissingVars returns the references that would interpolate to an empty string given values:
// no default, and no value supplied. Each missing name is reported once, at its first occurrence,
// sorted by name so the resulting message is stable.
//
// An empty value counts as unset. The deploy dialog sends every declared name whether it was filled
// in or not, so a presence check would never fire — and it matches Compose's own `:-` semantics,
// which treat set-but-empty as absent. A deliberately empty value is written `${VAR:-}`.
//
// `${VAR:?msg}` references are not reported: compose-go already fails on them with the author's own
// message, and reporting them here would only duplicate that.
func MissingVars(composeYAML string, values map[string]string) []VarRef {
	seen := make(map[string]VarRef)
	for _, ref := range ExtractVarRefs(composeYAML) {
		if ref.HasDefault || ref.Required {
			continue
		}
		if strings.TrimSpace(values[ref.Name]) != "" {
			continue
		}
		if _, dup := seen[ref.Name]; !dup {
			seen[ref.Name] = ref
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]VarRef, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// VarNames returns the names of the given references, in order.
func VarNames(refs []VarRef) []string {
	if len(refs) == 0 {
		return nil
	}
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return names
}

// walkScalars visits every scalar node in the tree, keys included — `${VAR}` is legal in a mapping
// key as well, and compose-go interpolates it there too.
func walkScalars(n *yaml.Node, visit func(*yaml.Node)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.ScalarNode {
		visit(n)
		return
	}
	for _, c := range n.Content {
		walkScalars(c, visit)
	}
}

// refsInScalar extracts the references from one scalar value. It scans left to right so that a `$$`
// escape consumes both characters and cannot be re-read as the start of a bare reference.
func refsInScalar(s string, line int) []VarRef {
	var refs []VarRef
	for i := 0; i < len(s); {
		if s[i] != '$' {
			i++
			continue
		}
		// `$$` is a literal dollar sign — skip both characters.
		if i+1 < len(s) && s[i+1] == '$' {
			i += 2
			continue
		}
		if m := varPattern.FindStringSubmatch(s[i:]); m != nil {
			hasDefault, required := classifyModifier(m[2])
			refs = append(refs, VarRef{Name: m[1], Line: line, HasDefault: hasDefault, Required: required})
			i += len(m[0])
			continue
		}
		if m := bareVarPattern.FindStringSubmatch(s[i:]); m != nil {
			refs = append(refs, VarRef{Name: m[1], Line: line})
			i += len(m[0])
			continue
		}
		i++
	}
	return refs
}

// classifyModifier interprets what follows the name inside `${...}`:
//
//	`-` / `:-`  default    — resolves to the default when no value is supplied
//	`?` / `:?`  required   — compose-go errors out with the author's own message
//	`+` / `:+`  alternate  — resolves to the alternate when set, to empty when not
//
// The alternate form is counted as "has a default": an empty result there is what the author asked
// for, so reporting it as unresolved would be a false positive. Anything else is a plain reference.
func classifyModifier(mod string) (hasDefault, required bool) {
	mod = strings.TrimPrefix(mod, ":")
	switch {
	case strings.HasPrefix(mod, "-"), strings.HasPrefix(mod, "+"):
		return true, false
	case strings.HasPrefix(mod, "?"):
		return false, true
	default:
		return false, false
	}
}
