export interface OpsRouteSwitchEvent {
  /** Position in the original diagnostic event array, not a synthetic order. */
  eventIndex: number
  groupName: string
  statusCode: number | null
  accountName: string
  message: string
}

function statusCode(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 100 || value > 599) return null
  return value
}

/** Only explicit backend event markers identify an error that caused a switch. */
export function parseOpsRouteSwitchEvents(payload: unknown): OpsRouteSwitchEvent[] {
  let events = payload
  if (typeof events === 'string') {
    if (!events.trim()) return []
    try {
      events = JSON.parse(events)
    } catch {
      return []
    }
  }
  if (!Array.isArray(events)) return []

  return events.flatMap((value: unknown, eventIndex): OpsRouteSwitchEvent[] => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return []
    const event = value as Record<string, unknown>
    const groupName = typeof event.route_switch_group === 'string' ? event.route_switch_group.trim() : ''
    if (!groupName) return []

    const accountName = typeof event.account_name === 'string' ? event.account_name.trim() : ''
    const accountId = typeof event.account_id === 'number' && Number.isSafeInteger(event.account_id) && event.account_id > 0
      ? String(event.account_id)
      : ''
    return [{
      eventIndex,
      groupName,
      statusCode: statusCode(event.upstream_status_code) ?? statusCode(event.status_code),
      accountName: accountName || accountId,
      message: typeof event.message === 'string' ? event.message : ''
    }]
  })
}
