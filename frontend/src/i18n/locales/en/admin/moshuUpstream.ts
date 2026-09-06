export default {
  moshuUpstream: {
    title: 'Moshu Upstream',
    description: 'View the Moshu group credentials, cost multipliers, and connection status assigned to L1.',
    restrictionTitle: 'This reseller station is locked to the Moshu upstream',
    restrictionBody: 'Administrators cannot add, import, edit, or delete other upstream accounts or proxies. Adjust retail multipliers under Groups; Moshu credentials are maintained by the station owner.',
    emptyTitle: 'No Moshu group is connected',
    emptyBody: 'Ask the station owner to assign the required Moshu group credential to this reseller station.',
    costMultiplier: 'Moshu cost multiplier',
    retailMultiplier: 'Retail multiplier',
    platform: 'Protocol platform',
    boundGroups: 'Bound retail groups',
    noBoundGroups: 'No retail group is bound',
    test: 'Test connection',
    testSuccess: 'Moshu upstream is reachable',
    testFailed: 'Moshu upstream connection test failed',
    loadFailed: 'Failed to load Moshu upstream details',
    status: {
      active: 'Active',
      inactive: 'Inactive',
      error: 'Error'
    }
  },
  moshuPricing: {
    title: 'Moshu Retail Pricing',
    description: 'Moshu cost multipliers are read-only. Set only the retail multiplier charged to your customers.',
    formulaTitle: 'Accounting formula',
    formula: 'Base model price × Moshu cost multiplier = upstream cost; base model price × retail multiplier = customer charge',
    productID: 'Product ID: {id}',
    costMultiplier: 'Moshu cost multiplier',
    costReadOnly: 'Defined by the Moshu credential and cannot be edited',
    retailMultiplier: 'Retail multiplier',
    userOverrides: 'Per-user multipliers',
    belowCostWarning: 'The retail multiplier is below the Moshu cost multiplier. Saving is blocked to prevent negative margin.',
    marginPreview: 'Multiplier spread per 1× base price: {margin}',
    descriptionLabel: 'Retail description',
    emptyTitle: 'No Moshu product is available for pricing',
    emptyBody: 'No product currently has both a Moshu credential and an L1 retail group.',
    loadFailed: 'Failed to load Moshu pricing',
    saveFailed: 'Failed to save the retail multiplier',
    saved: 'Retail multiplier saved'
  }
}
