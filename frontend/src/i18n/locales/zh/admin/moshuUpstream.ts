export default {
  moshuUpstream: {
    title: 'Moshu 上游',
    description: '查看 L1 已绑定的 Moshu 分组密钥、成本倍率和连接状态。',
    restrictionTitle: '此代理站已锁定为仅使用 Moshu 上游',
    restrictionBody: '管理员不能新增、导入、修改或删除其他上游账号和代理。销售倍率请到“分组管理”中调整；Moshu 密钥由站点所有者统一维护。',
    emptyTitle: '尚未绑定 Moshu 分组',
    emptyBody: '请由站点所有者为该代理站配置对应的 Moshu 分组密钥。',
    costMultiplier: 'Moshu 成本倍率',
    retailMultiplier: '销售倍率',
    platform: '协议平台',
    boundGroups: '已绑定销售分组',
    noBoundGroups: '未绑定销售分组',
    test: '测试连接',
    testSuccess: 'Moshu 上游连接正常',
    testFailed: 'Moshu 上游连接测试失败',
    loadFailed: '加载 Moshu 上游信息失败',
    status: {
      active: '正常',
      inactive: '停用',
      error: '异常'
    }
  },
  moshuPricing: {
    title: 'Moshu 二次定价',
    description: 'Moshu 成本倍率只读；你只需设置面向自己客户的销售倍率。',
    formulaTitle: '账务口径',
    formula: '标准模型价格 × Moshu 成本倍率 = 上游成本；标准模型价格 × 销售倍率 = 客户扣费',
    productID: '产品 ID：{id}',
    costMultiplier: 'Moshu 成本倍率',
    costReadOnly: '由 Moshu 密钥决定，不可修改',
    retailMultiplier: '销售倍率',
    userOverrides: '用户单独倍率',
    belowCostWarning: '销售倍率低于 Moshu 成本倍率，已禁止保存以避免倒挂。',
    marginPreview: '每 1 倍标准价的倍率差：{margin}',
    descriptionLabel: '销售说明',
    emptyTitle: '没有可定价的 Moshu 产品',
    emptyBody: '当前没有同时绑定 Moshu 密钥和 L1 销售分组的产品。',
    loadFailed: '加载 Moshu 定价失败',
    saveFailed: '保存销售倍率失败',
    saved: '销售倍率已保存'
  }
}
