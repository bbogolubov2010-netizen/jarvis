import { describe, expect, it } from 'vitest'
import { catalogUpdatedLabel, errorText, gameDuration, greeting } from './format'

describe('format helpers', () => {
  it('extracts messages from unknown errors', () => {
    expect(errorText(new Error('boom'))).toBe('boom')
    expect(errorText('plain')).toBe('plain')
    expect(errorText(42)).toBe('42')
  })

  it('greets according to the local hour', () => {
    expect(greeting(new Date(2026, 0, 1, 8))).toBe('Good morning')
    expect(greeting(new Date(2026, 0, 1, 14))).toBe('Good afternoon')
    expect(greeting(new Date(2026, 0, 1, 21))).toBe('Good evening')
  })

  it('formats game durations', () => {
    expect(gameDuration(0)).toBe('less than a minute')
    expect(gameDuration(59)).toBe('less than a minute')
    expect(gameDuration(60)).toBe('1 min')
    expect(gameDuration(59 * 60)).toBe('59 min')
    expect(gameDuration(60 * 60)).toBe('1 h')
    expect(gameDuration(90 * 60)).toBe('1 h 30 min')
  })

  it('labels an unscanned catalog and a scanned one', () => {
    expect(catalogUpdatedLabel(0)).toBe('not scanned yet')
    expect(catalogUpdatedLabel(Date.UTC(2026, 0, 1))).not.toBe('not scanned yet')
  })
})
