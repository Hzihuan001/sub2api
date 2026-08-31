import { describe, expect, it } from 'vitest'
import type { Group, GroupPlatform } from '@/types'
import { sortGroupsForKeySelection } from '../keyGroupOrder'

function group(id: number, name: string, platform: GroupPlatform): Group {
  return { id, name, platform } as Group
}

describe('API key group ordering', () => {
  it('clusters groups by platform catalog order and sorts names within each platform', () => {
    const source = [
      group(9, 'Gemini B', 'gemini'),
      group(7, 'OpenAI 10', 'openai'),
      group(4, 'Claude B', 'anthropic'),
      group(3, 'OpenAI 2', 'openai'),
      group(2, 'Claude A', 'anthropic'),
      group(8, 'Gemini A', 'gemini')
    ]

    expect(sortGroupsForKeySelection(source).map((item) => item.id)).toEqual([2, 4, 3, 7, 8, 9])
    expect(source.map((item) => item.id)).toEqual([9, 7, 4, 3, 2, 8])
  })

  it('uses the ID as a stable tie-breaker for identical names', () => {
    const source = [group(12, 'Production', 'openai'), group(5, 'production', 'openai')]
    expect(sortGroupsForKeySelection(source).map((item) => item.id)).toEqual([5, 12])
  })
})
