export default {
  moshuUpstream: {
    title: '上游管理',
    protocolDisabled: '代理商协议尚未启用',
    protocolDisabledHint: '当前继续使用第一阶段手工分组密钥。启用 MOSHU_RESELLER_CLIENT_ENABLED 后可使用接入向导。',
    enrollTitle: '接入 Moshu 代理商',
    enrollHint: '输入 Moshu 主站生成的一次性授权码；授权码兑换后立即失效。',
    baseUrl: 'Moshu 主站地址',
    enrollmentCode: '一次性授权码',
    connect: '连接并读取产品',
    connected: 'Moshu 代理商连接已建立',
    syncCatalog: '同步上游配置',
    catalogSynced: '上游配置已同步',
    authorizedProducts: '授权产品',
    revoked: '已撤销',
    salesName: '销售分组名称',
    enableSale: '创建销售分组',
    rotate: '轮换凭证',
    rotated: '产品凭证已轮换',
    saleActive: '销售已启用',
    savedResources: '已保留销售配置，重新启用时会复用。',
    stopSale: '停止销售',
    productSaved: '销售产品配置已保存',
    productStopped: '产品已停止销售',
    costMultiplier: '成本倍率',
    retailMultiplier: '销售倍率',
    loadFailed: '加载上游信息失败',
    status: {
      active: '正常',
      inactive: '停用',
      error: '异常'
    }
  }
}
