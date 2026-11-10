import { describe, expect, it } from 'vitest'
import { combinedPnl } from './portfolio'

describe('combinedPnl', () => {
  it('adds the server totals exactly, including losses', () => {
    expect(combinedPnl({ realizedPnl: -6000, unrealizedPnl: -900 })).toBe(-6900)
    expect(combinedPnl({ realizedPnl: 1057, unrealizedPnl: 15843 })).toBe(16900)
    expect(combinedPnl({ realizedPnl: 0, unrealizedPnl: 0 })).toBe(0)
  })

  it('refuses values it cannot add exactly', () => {
    expect(combinedPnl({ realizedPnl: 0.5, unrealizedPnl: 0 })).toBeNull()
    expect(combinedPnl({ realizedPnl: Number.NaN, unrealizedPnl: 0 })).toBeNull()
    expect(combinedPnl({ realizedPnl: Number.MAX_SAFE_INTEGER, unrealizedPnl: 1 })).toBeNull()
  })
})
