package service

func imagePriceConfigFromAPIKey(apiKey *APIKey) *ImagePriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	g := resellerMediaFallbackGroup(apiKey.Group)
	return &ImagePriceConfig{
		resellerPricing: apiKey.Group.resellerPricing,
		Price1K:         g.ImagePrice1K,
		Price2K:         g.ImagePrice2K,
		Price4K:         g.ImagePrice4K,
	}
}

func apiKeyHasConfiguredImagePrice(apiKey *APIKey, imageSize string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetImagePrice(imageSize) != nil
}

func videoPriceConfigFromAPIKey(apiKey *APIKey) *VideoPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	g := resellerMediaFallbackGroup(apiKey.Group)
	return &VideoPriceConfig{
		resellerPricing: apiKey.Group.resellerPricing,
		Price480P:       g.VideoPrice480P,
		Price720P:       g.VideoPrice720P,
		Price1080P:      g.VideoPrice1080P,
		ModelPrices:     g.VideoModelPrices,
	}
}

func apiKeyHasConfiguredVideoPrice(apiKey *APIKey, model, resolution string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetVideoPriceForModel(model, resolution) != nil
}

func webSearchPricePerCallFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return resellerMediaFallbackGroup(apiKey.Group).WebSearchPricePerCall
}

func groupSearchPricePer1kFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return resellerMediaFallbackGroup(apiKey.Group).GetSearchPricePer1k()
}

func groupAudioPriceConfigFromAPIKey(apiKey *APIKey) *audioPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	g := resellerMediaFallbackGroup(apiKey.Group)
	return &audioPriceConfig{
		RealtimePerMin: g.AudioRealtimePricePerMin,
		TTSPerMChars:   g.AudioTTSPricePerMillionChars,
		STTPerHour:     g.AudioSTTPricePerHour,
	}
}

// Nil is missing; an explicit local zero is a deliberate free retail price.
func resellerMediaFallbackGroup(local *Group) *Group {
	if local == nil || local.resellerPricing == nil {
		return local
	}
	g := *local
	p := local.resellerPricing.snapshot.Group
	fallback := func(dst **float64, upstream *float64) {
		if *dst == nil {
			*dst = cloneResellerFloat(upstream)
		}
	}
	fallback(&g.ImagePrice1K, p.ImagePrice1K)
	fallback(&g.ImagePrice2K, p.ImagePrice2K)
	fallback(&g.ImagePrice4K, p.ImagePrice4K)
	fallback(&g.VideoPrice480P, p.VideoPrice480P)
	fallback(&g.VideoPrice720P, p.VideoPrice720P)
	fallback(&g.VideoPrice1080P, p.VideoPrice1080P)
	fallback(&g.WebSearchPricePerCall, p.WebSearchPricePerCall)
	fallback(&g.SearchPricePer1k, p.SearchPricePer1k)
	fallback(&g.AudioRealtimePricePerMin, p.AudioRealtimePricePerMin)
	fallback(&g.AudioTTSPricePerMillionChars, p.AudioTTSPricePerMillionChars)
	fallback(&g.AudioSTTPricePerHour, p.AudioSTTPricePerHour)
	g.VideoModelPrices = make(map[string]map[string]float64)
	for model, prices := range p.VideoModelPrices {
		g.VideoModelPrices[model] = make(map[string]float64)
		for size, price := range prices {
			if local.GetVideoPriceForModel(model, size) == nil {
				g.VideoModelPrices[model][size] = price
			}
		}
	}
	for model, prices := range local.VideoModelPrices {
		if g.VideoModelPrices[model] == nil {
			g.VideoModelPrices[model] = make(map[string]float64)
		}
		for size, price := range prices {
			g.VideoModelPrices[model][size] = price
		}
	}
	return &g
}

func resellerUseTokenPricing(resolved *ResolvedPricing, localMediaPrice bool) bool {
	return resolved != nil && resolved.Mode == BillingModeToken && (!resolved.resellerFallback || !localMediaPrice)
}

func apiKeyHasConfiguredAudioPrice(key *APIKey, mode string) bool {
	if key == nil || key.Group == nil {
		return false
	}
	switch mode {
	case "stt":
		return key.Group.AudioSTTPricePerHour != nil
	case "tts":
		return key.Group.AudioTTSPricePerMillionChars != nil
	case "realtime":
		return key.Group.AudioRealtimePricePerMin != nil
	}
	return false
}
