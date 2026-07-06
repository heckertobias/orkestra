/**
 * Utilities for env-var handling in the Stack Editor.
 */

/**
 * Extract all ${VAR} / ${VAR:-default} / ${VAR:?err} / $VAR references
 * from a compose YAML string.  Returns a deduplicated, sorted list of variable names.
 */
export function extractComposeVars(yaml: string): string[] {
  const seen = new Set<string>()
  // Match ${VAR...} forms first
  for (const m of yaml.matchAll(/\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}/g)) {
    seen.add(m[1])
  }
  // Match bare $VAR forms (not preceded by another $ to avoid $$VAR escapes)
  for (const m of yaml.matchAll(/(?<!\$)\$([A-Za-z_][A-Za-z0-9_]*)/g)) {
    seen.add(m[1])
  }
  return [...seen].sort()
}

/**
 * Map each referenced variable to the 1-based line number(s) in the compose YAML
 * where it appears (deduplicated, in document order). Mirrors extractComposeVars.
 */
export function extractComposeVarLines(yaml: string): Record<string, number[]> {
  const out: Record<string, number[]> = {}
  yaml.split('\n').forEach((text, i) => {
    const ln = i + 1
    const add = (name: string) => {
      (out[name] ??= [])
      if (!out[name].includes(ln)) out[name].push(ln)
    }
    for (const m of text.matchAll(/\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}/g)) add(m[1])
    for (const m of text.matchAll(/(?<!\$)\$([A-Za-z_][A-Za-z0-9_]*)/g)) add(m[1])
  })
  return out
}

/**
 * Extract default values from compose variable references that provide one:
 * `${VAR:-default}` and `${VAR-default}`. The error forms (`${VAR:?err}` /
 * `${VAR?err}`) and plain `${VAR}` / `$VAR` are ignored. First occurrence wins.
 */
export function extractComposeVarDefaults(yaml: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const m of yaml.matchAll(/\$\{([A-Za-z_][A-Za-z0-9_]*):?-([^}]*)\}/g)) {
    if (!(m[1] in out)) out[m[1]] = m[2]
  }
  return out
}

/**
 * Render a .env template from a set of keys and their (possibly empty) values.
 * Keys without a value are written as KEY= (empty).
 */
export function toDotEnv(keys: string[], values: Record<string, string>): string {
  if (keys.length === 0) return ''
  return keys.map(k => `${k}=${values[k] ?? ''}`).join('\n') + '\n'
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * Rename a variable reference in a compose YAML string, preserving any
 * `${VAR:-default}` / `${VAR:?err}` modifier. Handles both `${VAR}` and bare
 * `$VAR` forms and leaves `$$VAR` escapes untouched.
 */
export function renameComposeVar(yaml: string, oldName: string, newName: string): string {
  if (!oldName || !newName || oldName === newName) return yaml
  const o = escapeRegExp(oldName)
  // ${VAR...} — only rewrite the name, keep the trailing modifier and brace.
  let out = yaml.replace(new RegExp(`\\$\\{${o}(?![A-Za-z0-9_])`, 'g'), `\${${newName}`)
  // bare $VAR (not part of $$VAR)
  out = out.replace(new RegExp(`(?<!\\$)\\$${o}(?![A-Za-z0-9_])`, 'g'), `$${newName}`)
  return out
}

/**
 * Remove every reference to a variable from a compose YAML string (both
 * `${VAR...}` and bare `$VAR` forms). Used when a referenced-only entry is
 * deleted from the variable list.
 */
export function removeComposeVar(yaml: string, name: string): string {
  if (!name) return yaml
  const n = escapeRegExp(name)
  let out = yaml.replace(new RegExp(`\\$\\{${n}(?![A-Za-z0-9_])[^}]*\\}`, 'g'), '')
  out = out.replace(new RegExp(`(?<!\\$)\\$${n}(?![A-Za-z0-9_])`, 'g'), '')
  return out
}
