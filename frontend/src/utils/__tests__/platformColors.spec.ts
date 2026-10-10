import { describe, expect, it } from 'vitest'
import {
  platformBadgeClass,
  platformGradientClass,
  platformTextClass
} from '../platformColors'

describe('platformColors', () => {
  it('uses the neutral theme for platforms that are not in the built-in catalog', () => {
    expect(platformBadgeClass('unknown_platform')).toContain('slate')
    expect(platformTextClass('unknown_platform')).toContain('primary')
    expect(platformGradientClass('unknown_platform')).toContain('primary')
  })
})
