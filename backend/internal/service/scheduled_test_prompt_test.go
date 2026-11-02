package service

import (
	"strings"
	"testing"
)

func TestDefaultPelicanTestPromptRequiresCompleteAnimatedSubject(t *testing.T) {
	prompt := DefaultPelicanTestPrompt
	for _, required := range []string{
		"complete head, beak, neck, body, both wings, tail, two legs, two feet",
		"all bicycle parts including frame, seat, handlebars, pedals",
		"Do not crop, hide, overlap, or omit any body part",
		"Return only the complete HTML document",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("default Pelican prompt missing %q", required)
		}
	}
}
