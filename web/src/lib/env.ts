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
 * Render a .env template from a set of keys and their (possibly empty) values.
 * Keys without a value are written as KEY= (empty).
 */
export function toDotEnv(keys: string[], values: Record<string, string>): string {
  if (keys.length === 0) return ''
  return keys.map(k => `${k}=${values[k] ?? ''}`).join('\n') + '\n'
}
