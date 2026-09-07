package reseller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// Hash the whole effective catalog, not the maximum individual price version.
// GeneratedAt is volatile; product timestamps and credential state are not.
func catalogETag(catalog Catalog) (string, error) {
	catalog.GeneratedAt = time.Time{}
	catalog.Products = append([]Product{}, catalog.Products...)
	sort.Slice(catalog.Products, func(i, j int) bool { return catalog.Products[i].ID < catalog.Products[j].ID })
	raw, err := json.Marshal(catalog)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return `W/"catalog-` + hex.EncodeToString(digest[:]) + `"`, nil
}
