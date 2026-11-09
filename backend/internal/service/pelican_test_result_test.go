package service

import (
	"testing"
	"time"
)

func TestNormalizePelicanTestPromptUpgradesOnlyDefaultPrompts(t *testing.T) {
	for _, prompt := range []string{"", LegacyDefaultPelicanTestPrompt, PreviousDefaultPelicanTestPrompt, DefaultPelicanTestPrompt} {
		got, validate := normalizePelicanTestPrompt(prompt)
		if got != DefaultPelicanTestPrompt || !validate {
			t.Errorf("normalizePelicanTestPrompt(%q) = (%q, %v)", prompt, got, validate)
		}
	}

	custom := "Generate a different artifact"
	got, validate := normalizePelicanTestPrompt(custom)
	if got != custom || validate {
		t.Fatalf("custom prompt normalized to (%q, %v)", got, validate)
	}
}

func TestValidatePelicanTestResultRequiresHTMLAndSVG(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{name: "tool refusal", response: "I cannot create files in this environment.", want: "failed"},
		{name: "HTML without SVG", response: "<html><body>done</body></html>", want: "failed"},
		{name: "complete source", response: "<!doctype html><html><body><svg><circle /></svg></body></html>", want: "success"},
		{name: "fenced source", response: "```html\n<html><svg></svg></html>\n```", want: "success"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := &ScheduledTestResult{Status: "success", ResponseText: tc.response}
			validatePelicanTestResult(result, true)
			if result.Status != tc.want {
				t.Fatalf("status = %q, want %q", result.Status, tc.want)
			}
			if tc.want == "failed" {
				if result.ErrorMessage != invalidPelicanArtifactError || result.ResponseText != tc.response {
					t.Fatalf("invalid artifact result lost diagnostics: %#v", result)
				}
			}
		})
	}
}

func TestPelicanResultsCleanupCutoffUsesLocalMidnight(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.November, 9, 0, 3, 0, 0, loc)
	got := pelicanResultsCleanupCutoff(now.UTC(), loc)
	want := time.Date(2026, time.November, 9, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("cutoff = %s, want %s", got, want)
	}
}
