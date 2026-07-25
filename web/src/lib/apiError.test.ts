import { describe, it, expect } from 'vitest'
import { apiError, errText, parseConnectError } from './apiError'

describe('apiError', () => {
  it('unwraps a Connect JSON error body', () => {
    expect(apiError('{"code":"invalid_argument","message":"name is required"}'))
      .toBe('name is required')
  })

  it('falls back to the raw text when the body is not JSON', () => {
    expect(apiError('502 Bad Gateway')).toBe('502 Bad Gateway')
  })

  it('falls back to the raw text when JSON carries no message', () => {
    expect(apiError('{"code":"internal"}')).toBe('{"code":"internal"}')
  })

  it('falls back when the message is empty', () => {
    expect(apiError('{"message":""}')).toBe('{"message":""}')
  })

  it('falls back when the message is not a string', () => {
    expect(apiError('{"message":42}')).toBe('{"message":42}')
  })

  it('returns a generic message for an empty body', () => {
    expect(apiError('')).toBe('Request failed')
  })

  it('survives a JSON body that is not an object', () => {
    expect(apiError('null')).toBe('null')
    expect(apiError('"just a string"')).toBe('"just a string"')
  })
})

describe('errText', () => {
  it('uses the message of an Error', () => {
    expect(errText(new Error('boom'))).toBe('boom')
  })

  it('stringifies non-Error values', () => {
    expect(errText('plain')).toBe('plain')
    expect(errText(404)).toBe('404')
    expect(errText(null)).toBe('null')
    expect(errText(undefined)).toBe('undefined')
  })

  it('keeps the message of an Error subclass', () => {
    class HttpError extends Error {}
    expect(errText(new HttpError('nope'))).toBe('nope')
  })
})

describe('parseConnectError', () => {
  it('unwraps a stringified Error wrapping a compact JSON body', () => {
    expect(parseConnectError('Error: {"code":"unauthenticated","message":"invalid credentials"}'))
      .toBe('invalid credentials')
  })

  it('unwraps a compact JSON body with no prefix', () => {
    expect(parseConnectError('{"message":"invalid credentials"}')).toBe('invalid credentials')
  })

  it('strips the Error prefix from a plain-text message', () => {
    expect(parseConnectError('Error: network unreachable')).toBe('network unreachable')
  })

  it('returns plain text unchanged when there is no prefix', () => {
    expect(parseConnectError('network unreachable')).toBe('network unreachable')
  })

  it('returns the input unchanged when the JSON has no message field', () => {
    expect(parseConnectError('Error: {"code":"internal"}'))
      .toBe('Error: {"code":"internal"}')
  })
})
