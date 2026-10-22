const ROUTE_SWITCH_LABEL_PREFIXES = ['切换至后续优先级分组：', '切换备用分组：']

export interface OpsRouteSwitchAnnotation {
  groupName: string
  cleanMessage: string
}

/**
 * The backend appends a routing annotation to the existing error message so
 * the database schema stays unchanged. Extract the terminal annotation before
 * the UI truncates long messages, while preserving the original message text.
 */
export function parseOpsRouteSwitchAnnotation(message: unknown): OpsRouteSwitchAnnotation | null {
  const value = String(message ?? '').trim()
  if (!value) return null

  let markerIndex = -1
  let markerPrefix = ''
  for (const prefix of ROUTE_SWITCH_LABEL_PREFIXES) {
    const index = value.lastIndexOf(prefix)
    if (index > markerIndex) {
      markerIndex = index
      markerPrefix = prefix
    }
  }
  if (markerIndex < 0) return null

  const hasMessagePrefix = markerIndex > 0
  if (hasMessagePrefix && !value.slice(0, markerIndex).endsWith(' · ')) return null

  const groupName = value.slice(markerIndex + markerPrefix.length).trim()
  if (!groupName) return null

  const separatorStart = hasMessagePrefix ? markerIndex - 3 : markerIndex
  return {
    groupName,
    cleanMessage: value.slice(0, separatorStart).trimEnd()
  }
}
