package reseller

import "github.com/google/wire"

var ProviderSet = wire.NewSet(NewServiceWithPricing, NewHandler)
