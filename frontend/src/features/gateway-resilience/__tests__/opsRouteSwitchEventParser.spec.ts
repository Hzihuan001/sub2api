import { describe, expect, it } from 'vitest'
import { parseOpsRouteSwitchEvents } from '../opsRouteSwitchEvents'

describe('explicit route-switch diagnostic events', () => {
  it.each([undefined, null, '', '  ', 'not json', '[', 'null', '{}', '{} ', '[]', 42, {}, true])(
    'ignores empty, malformed or non-array payload %j',
    payload => expect(parseOpsRouteSwitchEvents(payload)).toEqual([])
  )

  it('does not infer a switch from event order or an old message annotation', () => {
    expect(parseOpsRouteSwitchEvents(JSON.stringify([
      { message: '503 upstream overloaded' },
      { message: 'failed · 切换至后续优先级分组：备用组', upstream_status_code: 503 }
    ]))).toEqual([])
  })

  it('selects only explicitly marked events and preserves their original position and message', () => {
    const message = '  upstream overloaded\nplease retry  '
    const events = [
      { account_name: 'not switched', message: 'earlier error', upstream_status_code: 502 },
      { route_switch_group: ' GPT-企业 ', account_name: ' Account A ', upstream_status_code: 503, status_code: 502, message },
      { account_name: 'not switched either', message: 'later error', upstream_status_code: 504 },
      { route_switch_group: 'DeepSeek', account_id: 48, status_code: 429, message: 'rate limited' }
    ]
    expect(parseOpsRouteSwitchEvents(JSON.stringify(events))).toEqual([
      { eventIndex: 1, groupName: 'GPT-企业', statusCode: 503, accountName: 'Account A', message },
      { eventIndex: 3, groupName: 'DeepSeek', statusCode: 429, accountName: '48', message: 'rate limited' }
    ])
    expect(parseOpsRouteSwitchEvents(events)).toEqual(parseOpsRouteSwitchEvents(JSON.stringify(events)))
  })

  it('ignores invalid markers and keeps safe placeholders for malformed event fields', () => {
    expect(parseOpsRouteSwitchEvents([
      null, [], 'event',
      { route_switch_group: true }, { route_switch_group: {} }, { route_switch_group: ' ' },
      { route_switch_group: 'Backup', account_name: {}, account_id: -1, upstream_status_code: 600, status_code: '503', message: {} },
      { route_switch_group: 'Backup 2', upstream_status_code: 503.5, status_code: 502, message: 'failed' }
    ])).toEqual([
      { eventIndex: 6, groupName: 'Backup', accountName: '', statusCode: null, message: '' },
      { eventIndex: 7, groupName: 'Backup 2', accountName: '', statusCode: 502, message: 'failed' }
    ])
  })

  it('does not merge separate switch events with the same target group', () => {
    const result = parseOpsRouteSwitchEvents([
      { route_switch_group: 'Backup', message: 'first' },
      { route_switch_group: 'Backup', message: 'second' }
    ])
    expect(result.map(event => event.message)).toEqual(['first', 'second'])
  })
})
