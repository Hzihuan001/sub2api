export const zh = {
  title: '备用分组（可选）',
  description: '主组优先；需要备用时，按下列顺序，根据原请求的模型、接口和分组可调度性选择分组。最多添加 4 个备用分组。',
  billing: '费用按实际命中的分组及该组适用价格计算，可能与主组不同。',
  select: '添加备用分组',
  emptyOptions: '暂无其他可用的按量分组',
  noBackups: '未设置备用分组',
  choosePrimary: '请先选择一个主组。',
  standardOnlyHint: '多组路由仅支持按量分组。订阅分组可单独使用，不能与备用分组组合。',
  claudeCodeOnlyHint: 'Claude Code 专用分组可单独使用，不能添加备用分组。请选择其他主组以配置多组路由。',
  limitReached: '已达到 4 个备用分组上限。',
  position: '备用 {position}',
  moveUp: '上移 {name}',
  moveDown: '下移 {name}',
  remove: '移除 {name}',
  unavailable: '分组 #{id}（已不可用）',
  summary: '+{count} 个备用分组',
  adminEditHint: '此 Key 已配置备用分组。请在用户的 API Key 编辑页调整完整组合，快捷改单组不可用。',
  tooMany: '最多只能设置 4 个备用分组。',
  primaryRequired: '配置备用分组前，请先选择主组。',
  groupUnavailable: '组合包含无权限或已停用的分组，请移除或更换后保存。',
  standardOnly: '主组和所有备用分组都必须为按量分组；订阅分组不能参与组合。',
  claudeCodeOnly: '组合包含 Claude Code 专用分组。请更换该分组，或清空所有备用分组后再保存。',
  duplicate: '主组与备用分组不能重复，备用分组之间也不能重复。'
}

export const en: Record<keyof typeof zh, string> = {
  title: 'Backup groups (optional)',
  description: 'The primary group is preferred. When a backup is needed, groups are considered in this order using the original request model, endpoint, and scheduling availability. Add up to 4 backup groups.',
  billing: 'Charges use the group that actually handles the request and its applicable prices, which may differ from the primary group.',
  select: 'Add a backup group',
  emptyOptions: 'No other pay-as-you-go groups available',
  noBackups: 'No backup groups configured',
  choosePrimary: 'Choose a primary group first.',
  standardOnlyHint: 'Multiple groups require pay-as-you-go billing. A subscription group can be used on its own, but cannot be combined with backup groups.',
  claudeCodeOnlyHint: 'A Claude Code-only group can be used on its own, but cannot have backup groups. Choose another primary group to configure multi-group routing.',
  limitReached: 'The limit of 4 backup groups has been reached.',
  position: 'Backup {position}',
  moveUp: 'Move {name} up',
  moveDown: 'Move {name} down',
  remove: 'Remove {name}',
  unavailable: 'Group #{id} (unavailable)',
  summary: '+{count} backup groups',
  adminEditHint: 'This key has backup groups. Edit the complete route on the user API Keys page; quick group changes are unavailable.',
  tooMany: 'You can configure at most 4 backup groups.',
  primaryRequired: 'Choose a primary group before adding backup groups.',
  groupUnavailable: 'A selected group is unavailable or inactive. Remove or replace it before saving.',
  standardOnly: 'The primary and all backup groups must use pay-as-you-go billing. Subscription groups cannot be combined.',
  claudeCodeOnly: 'The route includes a Claude Code-only group. Replace that group or clear all backup groups before saving.',
  duplicate: 'The primary and backup groups must all be different.'
}

export function prefixedMessages(messages: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(messages).map(([key, value]) => [`custom.keyRouting.${key}`, value]))
}
