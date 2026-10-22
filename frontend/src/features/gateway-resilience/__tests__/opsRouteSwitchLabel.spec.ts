import { describe, expect, it } from 'vitest'
import { parseOpsRouteSwitchAnnotation } from '../opsRouteSwitchLabel'

describe('parseOpsRouteSwitchAnnotation', () => {
  it('extracts the terminal next-priority-group annotation and preserves the original error', () => {
    expect(parseOpsRouteSwitchAnnotation('upstream timed out · 切换至后续优先级分组：GPT-企业')).toEqual({
      groupName: 'GPT-企业',
      cleanMessage: 'upstream timed out'
    })
  })

  it('does not treat a mention inside the error body as a routing annotation', () => {
    expect(parseOpsRouteSwitchAnnotation('request mentions 切换至后续优先级分组：old text')).toBeNull()
  })

  it('continues to recognize historical annotations', () => {
    expect(parseOpsRouteSwitchAnnotation('upstream timed out · 切换备用分组：GPT-企业')).toEqual({
      groupName: 'GPT-企业',
      cleanMessage: 'upstream timed out'
    })
  })

  it('rejects empty messages and empty group names', () => {
    expect(parseOpsRouteSwitchAnnotation('')).toBeNull()
    expect(parseOpsRouteSwitchAnnotation('error · 切换至后续优先级分组：')).toBeNull()
  })
})
