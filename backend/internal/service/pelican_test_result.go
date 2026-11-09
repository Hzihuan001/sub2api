package service

import "strings"

const invalidPelicanArtifactError = "model response did not include a complete HTML document containing an SVG element"

func normalizePelicanTestPrompt(prompt string) (string, bool) {
	switch strings.TrimSpace(prompt) {
	case "", LegacyDefaultPelicanTestPrompt, PreviousDefaultPelicanTestPrompt, DefaultPelicanTestPrompt:
		return DefaultPelicanTestPrompt, true
	default:
		return prompt, false
	}
}

func validatePelicanTestResult(result *ScheduledTestResult, requireHTMLSVG bool) {
	if result == nil || !requireHTMLSVG || result.Status != "success" {
		return
	}
	if hasCompleteHTMLSVG(result.ResponseText) {
		return
	}
	result.Status = "failed"
	result.ErrorMessage = invalidPelicanArtifactError
}

func hasCompleteHTMLSVG(value string) bool {
	markup := strings.ToLower(value)
	htmlStart := strings.Index(markup, "<html")
	svgStart := strings.Index(markup, "<svg")
	svgEnd := strings.LastIndex(markup, "</svg>")
	htmlEnd := strings.LastIndex(markup, "</html>")
	return htmlStart >= 0 && svgStart > htmlStart && svgEnd > svgStart && htmlEnd > svgEnd
}
