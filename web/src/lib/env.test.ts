import { describe, it, expect } from 'vitest'
import {
  extractComposeVars,
  extractComposeVarLines,
  extractComposeVarDefaults,
  toDotEnv,
  renameComposeVar,
  removeComposeVar,
} from './env'

describe('extractComposeVars', () => {
  it('returns nothing for empty input', () => {
    expect(extractComposeVars('')).toEqual([])
    expect(extractComposeVars('services:\n  web:\n    image: nginx\n')).toEqual([])
  })

  it('extracts every ${VAR} form', () => {
    expect(extractComposeVars('${PLAIN}')).toEqual(['PLAIN'])
    expect(extractComposeVars('${WITH_DEFAULT:-nginx}')).toEqual(['WITH_DEFAULT'])
    expect(extractComposeVars('${WITH_DASH_DEFAULT-nginx}')).toEqual(['WITH_DASH_DEFAULT'])
    expect(extractComposeVars('${REQUIRED:?must be set}')).toEqual(['REQUIRED'])
  })

  it('extracts bare $VAR references', () => {
    expect(extractComposeVars('image: $IMAGE')).toEqual(['IMAGE'])
  })

  it('ignores $$VAR escapes', () => {
    expect(extractComposeVars('command: echo $$NOT_A_VAR')).toEqual([])
  })

  it('deduplicates and sorts', () => {
    expect(extractComposeVars('${ZULU} $ALPHA ${ZULU} $ALPHA')).toEqual(['ALPHA', 'ZULU'])
  })

  it('picks up a variable nested inside a default', () => {
    expect(extractComposeVars('${OUTER:-$INNER}')).toEqual(['INNER', 'OUTER'])
  })

  it('does not treat a leading digit as a variable', () => {
    expect(extractComposeVars('$1 ${2}')).toEqual([])
  })

  it('extracts from a realistic compose file', () => {
    const yaml = [
      'services:',
      '  web:',
      '    image: ${IMAGE:-nginx:latest}',
      '    ports:',
      '      - "${HTTP_PORT}:80"',
      '    environment:',
      '      DB_URL: $DATABASE_URL',
      '      LITERAL: $$NOT_A_VAR',
    ].join('\n')
    expect(extractComposeVars(yaml)).toEqual(['DATABASE_URL', 'HTTP_PORT', 'IMAGE'])
  })
})

describe('extractComposeVarLines', () => {
  it('maps variables to 1-based line numbers', () => {
    const yaml = 'a: ${X}\nb: nothing\nc: $Y'
    expect(extractComposeVarLines(yaml)).toEqual({ X: [1], Y: [3] })
  })

  it('records every line a variable appears on', () => {
    const yaml = 'a: ${X}\nb: ${X}\nc: ${X}'
    expect(extractComposeVarLines(yaml)).toEqual({ X: [1, 2, 3] })
  })

  it('deduplicates repeats within a single line', () => {
    expect(extractComposeVarLines('a: ${X}-${X}')).toEqual({ X: [1] })
  })

  it('ignores $$VAR escapes, matching extractComposeVars', () => {
    expect(extractComposeVarLines('a: $$NOPE')).toEqual({})
  })

  it('returns an empty map for input without references', () => {
    expect(extractComposeVarLines('services:\n  web:\n')).toEqual({})
  })
})

describe('extractComposeVarDefaults', () => {
  it('reads both default syntaxes', () => {
    expect(extractComposeVarDefaults('${A:-one}')).toEqual({ A: 'one' })
    expect(extractComposeVarDefaults('${B-two}')).toEqual({ B: 'two' })
  })

  it('ignores forms that carry no default', () => {
    expect(extractComposeVarDefaults('${A}')).toEqual({})
    expect(extractComposeVarDefaults('$A')).toEqual({})
    expect(extractComposeVarDefaults('${A:?required}')).toEqual({})
    expect(extractComposeVarDefaults('${A?required}')).toEqual({})
  })

  it('keeps an empty default', () => {
    expect(extractComposeVarDefaults('${A:-}')).toEqual({ A: '' })
  })

  it('lets the first occurrence win', () => {
    expect(extractComposeVarDefaults('${A:-first} ${A:-second}')).toEqual({ A: 'first' })
  })

  it('preserves defaults containing dashes and colons', () => {
    expect(extractComposeVarDefaults('${IMAGE:-nginx:1.25-alpine}')).toEqual({
      IMAGE: 'nginx:1.25-alpine',
    })
  })
})

describe('toDotEnv', () => {
  it('returns an empty string for no keys', () => {
    expect(toDotEnv([], {})).toBe('')
    expect(toDotEnv([], { A: '1' })).toBe('')
  })

  it('writes keys without a value as empty', () => {
    expect(toDotEnv(['A'], {})).toBe('A=\n')
  })

  it('writes values and terminates with a newline', () => {
    expect(toDotEnv(['A', 'B'], { A: '1', B: '2' })).toBe('A=1\nB=2\n')
  })

  it('mixes set and unset keys in the given order', () => {
    expect(toDotEnv(['B', 'A'], { A: '1' })).toBe('B=\nA=1\n')
  })
})

describe('renameComposeVar', () => {
  it('renames the ${VAR} form', () => {
    expect(renameComposeVar('image: ${OLD}', 'OLD', 'NEW')).toBe('image: ${NEW}')
  })

  it('preserves the default modifier', () => {
    expect(renameComposeVar('${OLD:-nginx}', 'OLD', 'NEW')).toBe('${NEW:-nginx}')
    expect(renameComposeVar('${OLD:?required}', 'OLD', 'NEW')).toBe('${NEW:?required}')
  })

  it('renames the bare $VAR form', () => {
    expect(renameComposeVar('image: $OLD', 'OLD', 'NEW')).toBe('image: $NEW')
  })

  it('leaves $$VAR escapes untouched', () => {
    expect(renameComposeVar('echo $$OLD', 'OLD', 'NEW')).toBe('echo $$OLD')
  })

  it('does not rename variables that merely share a prefix', () => {
    expect(renameComposeVar('${OLD_HOST} $OLD_PORT', 'OLD', 'NEW')).toBe('${OLD_HOST} $OLD_PORT')
  })

  it('renames every occurrence', () => {
    expect(renameComposeVar('${OLD} $OLD ${OLD:-x}', 'OLD', 'NEW')).toBe('${NEW} $NEW ${NEW:-x}')
  })

  it('is a no-op for empty or unchanged names', () => {
    expect(renameComposeVar('${OLD}', 'OLD', 'OLD')).toBe('${OLD}')
    expect(renameComposeVar('${OLD}', '', 'NEW')).toBe('${OLD}')
    expect(renameComposeVar('${OLD}', 'OLD', '')).toBe('${OLD}')
  })

  it('escapes regex metacharacters in the old name', () => {
    // Not a legal compose variable, but the helper must not build a broken regex.
    expect(renameComposeVar('${A}', 'A.B', 'C')).toBe('${A}')
  })
})

describe('removeComposeVar', () => {
  it('removes the whole ${VAR} reference including its default', () => {
    expect(removeComposeVar('image: ${GONE}', 'GONE')).toBe('image: ')
    expect(removeComposeVar('image: ${GONE:-nginx}', 'GONE')).toBe('image: ')
    expect(removeComposeVar('image: ${GONE:?required}', 'GONE')).toBe('image: ')
  })

  it('removes the bare $VAR form', () => {
    expect(removeComposeVar('image: $GONE', 'GONE')).toBe('image: ')
  })

  it('leaves $$VAR escapes untouched', () => {
    expect(removeComposeVar('echo $$GONE', 'GONE')).toBe('echo $$GONE')
  })

  it('does not remove variables that merely share a prefix', () => {
    expect(removeComposeVar('${GONE_HOST} $GONE_PORT', 'GONE')).toBe('${GONE_HOST} $GONE_PORT')
  })

  it('removes every occurrence', () => {
    expect(removeComposeVar('${GONE} $GONE', 'GONE')).toBe(' ')
  })

  it('is a no-op for an empty name', () => {
    expect(removeComposeVar('${GONE}', '')).toBe('${GONE}')
  })

  it('leaves the variable extractable as nothing afterwards', () => {
    const yaml = 'image: ${GONE:-nginx}\nport: ${KEEP}'
    const out = removeComposeVar(yaml, 'GONE')
    expect(extractComposeVars(out)).toEqual(['KEEP'])
  })
})
