export default {
  moshuUpstream: {
    title: '上游管理',
    emptyTitle: '尚未绑定上游分组',
    emptyBody: '当前没有可用的上游配置。',
    accountLabel: '上游账号 #{id}',
    costMultiplier: '成本倍率',
    retailMultiplier: '销售倍率',
    platform: '协议平台',
    boundGroups: '已绑定销售分组',
    noBoundGroups: '未绑定销售分组',
    test: '测试连接',
    testSuccess: '上游连接正常',
    testFailed: '上游连接测试失败',
    loadFailed: '加载上游信息失败',
    status: {
      active: '正常',
      inactive: '停用',
      error: '异常'
    }
  },
  moshuPricing: {
    title: '二次定价',
    description: '成本倍率只读；你只需设置面向自己客户的销售倍率。',
    formulaTitle: '账务口径',
    formula: '标准模型价格 × 成本倍率 = 上游成本；标准模型价格 × 销售倍率 = 客户扣费',
    productID: '产品 ID：{id}',
    costMultiplier: '成本倍率',
    costReadOnly: '由上游配置决定，不可修改',
    retailMultiplier: '销售倍率',
    userOverrides: '用户单独倍率',
    belowCostWarning: '销售倍率低于成本倍率，已禁止保存以避免倒挂。',
    marginPreview: '每 1 倍标准价的倍率差：{margin}',
    descriptionLabel: '销售说明',
    emptyTitle: '没有可定价的产品',
    emptyBody: '当前没有同时绑定上游账号和销售分组的产品。',
    loadFailed: '加载定价信息失败',
    saveFailed: '保存销售倍率失败',
    saved: '销售倍率已保存'
  }
}
