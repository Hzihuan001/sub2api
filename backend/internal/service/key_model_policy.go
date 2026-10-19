package service

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	MaxKeyModelPolicyModels   = 200
	MaxKeyModelPolicyIDLength = 200
)

// KeyModelPolicy narrows a key to exact client-facing model IDs. It never
// grants access beyond the selected group's own model and account policies.
type KeyModelPolicy struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models"`
}

func NormalizeKeyModelPolicy(policy *KeyModelPolicy) (*KeyModelPolicy, error) {
	if policy == nil || !policy.Enabled {
		return nil, nil
	}
	invalid := func(message string) (*KeyModelPolicy, error) {
		return nil, infraerrors.BadRequest("INVALID_KEY_MODEL_POLICY", message)
	}
	if len(policy.Models) == 0 || len(policy.Models) > MaxKeyModelPolicyModels {
		return invalid("enabled key model policy requires between 1 and 200 exact model IDs")
	}
	out := &KeyModelPolicy{Enabled: true, Models: make([]string, 0, len(policy.Models))}
	seen := make(map[string]struct{}, len(policy.Models))
	for _, raw := range policy.Models {
		model := strings.TrimSpace(raw)
		if model == "" || !utf8.ValidString(model) || utf8.RuneCountInString(model) > MaxKeyModelPolicyIDLength || strings.Contains(model, "*") || strings.ContainsFunc(model, unicode.IsControl) {
			return invalid("model IDs must be non-empty, at most 200 characters and must not contain wildcards or control characters")
		}
		if _, exists := seen[model]; !exists {
			seen[model] = struct{}{}
			out.Models = append(out.Models, model)
		}
	}
	return out, nil
}

func (p *KeyModelPolicy) Validate() error {
	_, err := NormalizeKeyModelPolicy(p)
	return err
}

func (p *KeyModelPolicy) Clone() *KeyModelPolicy {
	if p == nil {
		return nil
	}
	return &KeyModelPolicy{Enabled: p.Enabled, Models: append([]string(nil), p.Models...)}
}

func (p *KeyModelPolicy) Allows(model string) bool {
	if p == nil || !p.Enabled {
		return true
	}
	model = strings.TrimSpace(model)
	for _, allowed := range p.Models {
		if model != "" && model == allowed {
			return true
		}
	}
	return false
}

// UnmarshalJSON validates the public payload as well as stored/cache snapshots.
// No alias, case folding or wildcard expansion is applied to exact IDs.
func (p *KeyModelPolicy) UnmarshalJSON(data []byte) error {
	type plain KeyModelPolicy
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value := KeyModelPolicy(decoded)
	normalized, err := NormalizeKeyModelPolicy(&value)
	if err != nil {
		return err
	}
	if normalized == nil {
		*p = KeyModelPolicy{}
	} else {
		*p = *normalized
	}
	return nil
}

func (k *APIKey) KeyModelPolicyEnabled() bool {
	return k != nil && k.KeyModelPolicy != nil && k.KeyModelPolicy.Enabled
}

func (k *APIKey) AllowsPublicModel(model string) bool {
	return k == nil || k.KeyModelPolicy.Allows(model)
}

// KeyModelCandidatesForGroup exposes only explicit public IDs from an enabled
// group allowlist. It is not an inferred provider catalog or a permission
// grant, and never exposes wildcard policies, account IDs or upstream aliases.
func KeyModelCandidatesForGroup(group *Group) []string {
	if group == nil || !group.ModelAllowlistEnabled() {
		return nil
	}
	var models []string
	seen := make(map[string]struct{})
	for _, model := range group.ModelAllowlist.Models {
		one, err := NormalizeKeyModelPolicy(&KeyModelPolicy{Enabled: true, Models: []string{model}})
		if err != nil {
			continue
		}
		id := one.Models[0]
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			models = append(models, id)
		}
	}
	return models
}
