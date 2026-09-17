package reseller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestBuildPricingEnvelopeV2DigestCoversExactPayload(t *testing.T) {
	catalog := &PricingCatalog{Schema: 1, ResellerID: 9, Revision: "legacy", Products: map[int64]*service.ResellerPricingSnapshot{}}
	envelope, err := buildPricingEnvelopeV2(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Schema != 2 || envelope.BillingSemanticsVersion != 1 {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	hash := sha256.Sum256(envelope.Payload)
	if envelope.Digest != hex.EncodeToString(hash[:]) {
		t.Fatal("digest does not cover exact payload bytes")
	}
	var restored PricingCatalog
	if err := json.Unmarshal(envelope.Payload, &restored); err != nil || restored.ResellerID != catalog.ResellerID {
		t.Fatalf("payload roundtrip failed: %v", err)
	}
}
