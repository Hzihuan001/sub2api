package service

import "testing"

func TestScheduledTestPayloadThinkingEffortMapping(t *testing.T) {
	tests := []struct {
		name   string
		build  func(effort string) (map[string]any, error)
		field  string
		nested string
	}{
		{
			name: "anthropic messages",
			build: func(effort string) (map[string]any, error) {
				return createTestPayloadWithPromptAndEffort("claude-opus-5-5", "test", effort)
			},
			field: "output_config", nested: "effort",
		},
		{
			name: "openai responses",
			build: func(effort string) (map[string]any, error) {
				return createOpenAITestPayloadWithPromptAndEffort("gpt-6.1-sol", false, "test", effort), nil
			},
			field: "reasoning", nested: "effort",
		},
		{
			name: "openai chat completions",
			build: func(effort string) (map[string]any, error) {
				return createOpenAIChatCompletionsTestPayloadWithEffort("gpt-6.1-sol", "test", effort), nil
			},
			field: "reasoning_effort",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, defaultValue := range []string{"", "default", "unsupported"} {
				payload, err := tt.build(defaultValue)
				if err != nil {
					t.Fatalf("build default payload (%q): %v", defaultValue, err)
				}
				if _, exists := payload[tt.field]; exists {
					t.Errorf("default effort %q unexpectedly sent %q: %#v", defaultValue, tt.field, payload[tt.field])
				}
			}

			for _, effort := range []string{"low", "medium", "high", "xhigh"} {
				payload, err := tt.build(effort)
				if err != nil {
					t.Fatalf("build %s payload: %v", effort, err)
				}
				if tt.nested == "" {
					if got, ok := payload[tt.field].(string); !ok || got != effort {
						t.Errorf("%s = %#v, want %q", tt.field, payload[tt.field], effort)
					}
					continue
				}
				options, ok := payload[tt.field].(map[string]any)
				if !ok {
					t.Fatalf("%s = %#v, want object containing %s", tt.field, payload[tt.field], tt.nested)
				}
				if got, ok := options[tt.nested].(string); !ok || got != effort {
					t.Errorf("%s.%s = %#v, want %q", tt.field, tt.nested, options[tt.nested], effort)
				}
			}
		})
	}
}

func TestSupportedScheduledThinkingEffortUsesMappedNativeModelsOnly(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		model   string
		request string
		want    string
	}{
		{
			name: "official anthropic mapped model",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"base_url":      "https://api.anthropic.com",
				"model_mapping": map[string]any{"pelican": "claude-opus-5-5"},
			}},
			model: "pelican", request: "xhigh", want: "xhigh",
		},
		{
			name:    "kiro does not receive anthropic effort fields",
			account: &Account{Platform: PlatformKiro, Type: AccountTypeAPIKey},
			model:   "claude-opus-5-5", request: "high", want: "default",
		},
		{
			name: "openai mapped reasoning model",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"base_url":      "https://api.openai.com/v1",
				"model_mapping": map[string]any{"pelican": "gpt-6.1-sol"},
			}},
			model: "pelican", request: "xhigh", want: "xhigh",
		},
		{
			name: "xhigh is not sent to unverified openai model families",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"base_url": "https://api.openai.com/v1",
			}},
			model: "gpt-5.6-sol", request: "xhigh", want: "default",
		},
		{
			name: "openai compatible custom endpoint is left alone",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"base_url": "https://openai-compatible.example/v1",
			}},
			model: "gpt-6.1-sol", request: "high", want: "default",
		},
		{
			name: "non reasoning model is left alone",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"base_url": "https://api.openai.com/v1",
			}},
			model: "gpt-4.1", request: "high", want: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportedScheduledThinkingEffort(tt.account, tt.model, tt.request); got != tt.want {
				t.Fatalf("thinking effort = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeScheduledTestThinkingEffort(t *testing.T) {
	for _, effort := range []string{"default", "low", "medium", "high", "xhigh"} {
		t.Run(effort, func(t *testing.T) {
			plan := &ScheduledTestPlan{ThinkingEffort: effort}
			if err := normalizeScheduledTestThinkingEffort(plan); err != nil {
				t.Fatalf("normalize %q: %v", effort, err)
			}
			if plan.ThinkingEffort != effort {
				t.Fatalf("got %q, want %q", plan.ThinkingEffort, effort)
			}
		})
	}

	plan := &ScheduledTestPlan{ThinkingEffort: "  XHIGH  "}
	if err := normalizeScheduledTestThinkingEffort(plan); err != nil {
		t.Fatalf("normalize mixed-case xhigh: %v", err)
	}
	if plan.ThinkingEffort != "xhigh" {
		t.Fatalf("normalized effort = %q, want xhigh", plan.ThinkingEffort)
	}

	if err := normalizeScheduledTestThinkingEffort(&ScheduledTestPlan{ThinkingEffort: "max"}); err == nil {
		t.Fatal("expected unsupported effort to be rejected")
	}
}
