export default {
  moshuUpstream: {
    title: 'Upstream',
    emptyTitle: 'No upstream group is connected',
    emptyBody: 'No upstream configuration is currently available.',
    accountLabel: 'Upstream account #{id}',
    costMultiplier: 'Cost multiplier',
    retailMultiplier: 'Retail multiplier',
    platform: 'Protocol platform',
    boundGroups: 'Bound retail groups',
    noBoundGroups: 'No retail group is bound',
    test: 'Test connection',
    testSuccess: 'Upstream connection is healthy',
    testFailed: 'Upstream connection test failed',
    loadFailed: 'Failed to load upstream details',
    status: {
      active: 'Active',
      inactive: 'Inactive',
      error: 'Error'
    }
  },
  moshuPricing: {
    title: 'Retail Pricing',
    description: 'Cost multipliers are read-only. Set only the retail multiplier charged to your customers.',
    formulaTitle: 'Accounting formula',
    formula: 'Base model price × cost multiplier = upstream cost; base model price × retail multiplier = customer charge',
    productID: 'Product ID: {id}',
    costMultiplier: 'Cost multiplier',
    costReadOnly: 'Defined by the upstream configuration and cannot be edited',
    retailMultiplier: 'Retail multiplier',
    userOverrides: 'Per-user multipliers',
    belowCostWarning: 'The retail multiplier is below the cost multiplier. Saving is blocked to prevent negative margin.',
    marginPreview: 'Multiplier spread per 1× base price: {margin}',
    descriptionLabel: 'Retail description',
    emptyTitle: 'No product is available for pricing',
    emptyBody: 'No product currently has both an upstream account and a retail group.',
    loadFailed: 'Failed to load pricing',
    saveFailed: 'Failed to save the retail multiplier',
    saved: 'Retail multiplier saved'
  }
}
