package dto

import (
	"encoding/json"
	"testing"
)

func TestCustomMenuItemPassAuthContextJSONCompatibility(t *testing.T) {
	t.Run("legacy item keeps field unspecified", func(t *testing.T) {
		items := ParseCustomMenuItems(`[{"id":"legacy","label":"Legacy","icon_svg":"","url":"https://example.com","visibility":"user","sort_order":0}]`)
		if len(items) != 1 {
			t.Fatalf("expected one item, got %d", len(items))
		}
		if items[0].PassAuthContext != nil {
			t.Fatalf("expected legacy item to keep pass_auth_context unspecified")
		}
	})

	t.Run("explicit false survives round trip", func(t *testing.T) {
		items := ParseCustomMenuItems(`[{"id":"store","label":"Store","icon_svg":"","url":"https://catfk.com/shop/9Y18MT8C","pass_auth_context":false,"visibility":"user","sort_order":0}]`)
		if len(items) != 1 || items[0].PassAuthContext == nil || *items[0].PassAuthContext {
			t.Fatalf("expected explicit pass_auth_context=false")
		}

		raw, err := json.Marshal(items)
		if err != nil {
			t.Fatalf("marshal custom menu items: %v", err)
		}
		var decoded []map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode custom menu items: %v", err)
		}
		if value, ok := decoded[0]["pass_auth_context"]; !ok || value != false {
			t.Fatalf("expected pass_auth_context=false in JSON, got %#v", decoded[0]["pass_auth_context"])
		}
	})
}
