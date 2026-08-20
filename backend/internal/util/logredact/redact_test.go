package logredact

import (
	"strings"
	"testing"
)

func TestRedactText_JSONLike(t *testing.T) {
	in := `{"access_token":"ya29.a0AfH6SMDUMMY","refresh_token":"1//0gDUMMY","other":"ok"}`
	out := RedactText(in)
	if out == in {
		t.Fatalf("expected redaction, got unchanged")
	}
	if want := `"access_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
	if want := `"refresh_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
}

func TestRedactText_QueryLike(t *testing.T) {
	in := "access_token=ya29.a0AfH6SMDUMMY refresh_token=1//0gDUMMY"
	out := RedactText(in)
	if strings.Contains(out, "ya29") || strings.Contains(out, "1//0") {
		t.Fatalf("expected tokens redacted, got %q", out)
	}
}

func TestRedactText_GOCSPX(t *testing.T) {
	in := "client_secret=GOCSPX-your-client-secret"
	out := RedactText(in)
	if strings.Contains(out, "your-client-secret") {
		t.Fatalf("expected secret redacted, got %q", out)
	}
	if !strings.Contains(out, "client_secret=***") {
		t.Fatalf("expected key redacted, got %q", out)
	}
}

func TestRedactText_ExtraKeyCacheUsesNormalizedSortedKey(t *testing.T) {
	clearExtraTextPatternCache()

	out1 := RedactText("custom_secret=abc", "Custom_Secret", " custom_secret ")
	out2 := RedactText("custom_secret=xyz", "custom_secret")
	if !strings.Contains(out1, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in first call, got %q", out1)
	}
	if !strings.Contains(out2, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in second call, got %q", out2)
	}

	if got := countExtraTextPatternCacheEntries(); got != 1 {
		t.Fatalf("expected 1 cached pattern set, got %d", got)
	}
}

func TestRedactText_DefaultPathDoesNotUseExtraCache(t *testing.T) {
	clearExtraTextPatternCache()

	out := RedactText("access_token=abc")
	if !strings.Contains(out, "access_token=***") {
		t.Fatalf("expected default key redacted, got %q", out)
	}
	if got := countExtraTextPatternCacheEntries(); got != 0 {
		t.Fatalf("expected extra cache to remain empty, got %d", got)
	}
}

func TestRedactJSON_FoldsCamelAndKebabKeys(t *testing.T) {
	// Go structs and browser payloads spell the same credential three ways;
	// only the snake_case form used to be redacted.
	in := `{"accessToken":"ya29.DUMMY","refresh-token":"1//0gDUMMY",` +
		`"webSessionToken":"ws-DUMMY","apiKey":"crsr_DUMMY","note":"keep"}`
	out := RedactJSON([]byte(in))

	for _, leak := range []string{"ya29.DUMMY", "1//0gDUMMY", "ws-DUMMY", "crsr_DUMMY"} {
		if strings.Contains(out, leak) {
			t.Fatalf("expected %q redacted, got %q", leak, out)
		}
	}
	if !strings.Contains(out, `"note":"keep"`) {
		t.Fatalf("expected non-sensitive field preserved, got %q", out)
	}
}

func TestRedactText_MatchesCamelCaseKeys(t *testing.T) {
	out := RedactText("accessToken: ya29.DUMMY, webSessionToken=ws-DUMMY")
	if strings.Contains(out, "ya29.DUMMY") || strings.Contains(out, "ws-DUMMY") {
		t.Fatalf("expected camelCase keys redacted, got %q", out)
	}
}

func TestRedactText_ExtraSnakeKeyStillMatchesLiteralForm(t *testing.T) {
	clearExtraTextPatternCache()

	// Callers pass snake_case extras (RedactAuditQuery does); folding must not
	// stop those from matching their literal spelling.
	out := RedactText("custom_token=abc", "custom_token")
	if !strings.Contains(out, "custom_token=***") {
		t.Fatalf("expected extra key redacted, got %q", out)
	}
}

func clearExtraTextPatternCache() {
	extraTextPatternCache.Range(func(key, value any) bool {
		extraTextPatternCache.Delete(key)
		return true
	})
}

func countExtraTextPatternCacheEntries() int {
	count := 0
	extraTextPatternCache.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
