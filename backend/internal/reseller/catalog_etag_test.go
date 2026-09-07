package reseller

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCatalogETagTracksEntireCatalog(t *testing.T) {
	base := Catalog{ResellerID: 1, CatalogVersion: 10, Products: []Product{{ID: 1, PriceCatalogVersion: 10}, {ID: 2, PriceCatalogVersion: 1}}}
	original, err := catalogETag(base)
	require.NoError(t, err)
	for _, change := range []func(*Catalog){
		func(c *Catalog) { c.Products = append(c.Products, Product{ID: 3, PriceCatalogVersion: 1}) },
		func(c *Catalog) { c.Products = c.Products[:1] },
		func(c *Catalog) { c.Products[1].PriceCatalogVersion++ },
		func(c *Catalog) { c.Products[1].Models = []string{"new-model"} },
		func(c *Catalog) { c.Products[1].CredentialConfigured = true },
	} {
		candidate := base
		candidate.Products = append([]Product{}, base.Products...)
		change(&candidate)
		got, err := catalogETag(candidate)
		require.NoError(t, err)
		require.NotEqual(t, original, got)
	}
	base.GeneratedAt = time.Now()
	base.Products[0], base.Products[1] = base.Products[1], base.Products[0]
	got, err := catalogETag(base)
	require.NoError(t, err)
	require.Equal(t, original, got)
}
