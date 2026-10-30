package modelnormalize

import (
	"fmt"
	"regexp"
	"strings"
)

var protectedFunctionalTokens = map[string]struct{}{
	"air": {}, "audio": {}, "base": {}, "chat": {}, "coder": {},
	"embed": {}, "embedding": {}, "fast": {}, "flash": {}, "image": {},
	"instruct": {}, "large": {}, "lite": {}, "max": {}, "medium": {},
	"mini": {}, "micro": {}, "nano": {}, "non-reasoning": {}, "omni": {}, "plus": {},
	"pro": {}, "reasoning": {}, "rerank": {}, "reranker": {}, "search": {},
	"searching": {}, "small": {}, "think": {}, "thinking": {}, "tiny": {},
	"turbo": {}, "tts": {}, "video": {}, "vision": {}, "vl": {},
	"big":     {},
	"preview": {}, "beta": {}, "8k": {}, "16k": {}, "32k": {}, "64k": {},
	"128k": {}, "192k": {}, "256k": {}, "512k": {},
}

// RuleDiagnostic is a non-mutating explanation of a rule that needs review.
// Legacy settings can still be read by the runtime, while new saves reject
// diagnostics with severity "error".
type RuleDiagnostic struct {
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Index     int    `json:"index,omitempty"`
	Variant   string `json:"variant,omitempty"`
	Canonical string `json:"canonical,omitempty"`
}

// ValidateRules checks the invariants required by a persisted rule set.
// Existing legacy settings remain readable by the runtime; callers that save
// a new rule set should reject these errors before persistence.
func ValidateRules(rules Rules) error {
	if err := validateRuleStrings("router prefix", rules.RouterPrefixes, func(value string) bool {
		return strings.Contains(value, "/") || !hasPrefixBoundary(value)
	}); err != nil {
		return err
	}
	for index, suffix := range rules.FunctionalSuffixes {
		if err := validateFunctionalSuffix(suffix); err != nil {
			return fmt.Errorf("functional suffix #%d: %w", index+1, err)
		}
	}

	mappingsByKey := make(map[string]string, len(rules.ExplicitMappings)*2)
	for index, mapping := range rules.ExplicitMappings {
		variant := strings.TrimSpace(mapping.Variant)
		canonical := strings.ToLower(strings.TrimSpace(mapping.Canonical))
		if variant == "" || canonical == "" {
			return fmt.Errorf("explicit mapping #%d must have non-empty variant and canonical", index+1)
		}
		if strings.ContainsAny(canonical, "/[]") || strings.ContainsAny(canonical, "\r\n\t ") {
			return fmt.Errorf("explicit mapping #%d canonical must be a plain model name", index+1)
		}
		if normalizeWithoutExplicit(canonical, rules) != canonical {
			return fmt.Errorf("explicit mapping #%d canonical %q is changed by the active normalization rules", index+1, canonical)
		}

		for _, key := range []string{
			dotDashKey(normalizeToRouterBase(variant, rules)),
			dotDashKey(normalizeToBase(variant, rules)),
		} {
			if key == "" {
				return fmt.Errorf("explicit mapping #%d variant normalizes to an empty name", index+1)
			}
			if previous, ok := mappingsByKey[key]; ok && previous != canonical {
				return fmt.Errorf("explicit mapping conflict for %q: %q vs %q", key, previous, canonical)
			}
			mappingsByKey[key] = canonical
		}
	}

	return nil
}

func DiagnoseRules(rules Rules) []RuleDiagnostic {
	diagnostics := make([]RuleDiagnostic, 0)
	for index, prefix := range rules.RouterPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: "error", Kind: "empty_prefix", Message: "router prefix must not be empty", Index: index + 1,
			})
			continue
		}
		if strings.Contains(prefix, "/") {
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: "error", Kind: "path_prefix", Message: "router prefix must not contain a path", Index: index + 1, Variant: prefix,
			})
		} else if !hasPrefixBoundary(prefix) {
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: "error", Kind: "unsafe_prefix", Message: "router prefix must end at a model-name boundary", Index: index + 1, Variant: prefix,
			})
		}
	}
	for index, suffix := range rules.FunctionalSuffixes {
		suffix = strings.TrimSpace(suffix)
		if err := validateFunctionalSuffix(suffix); err != nil {
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: "error", Kind: "unsafe_suffix", Message: err.Error(), Index: index + 1, Variant: suffix,
			})
		}
	}
	for index, mapping := range rules.ExplicitMappings {
		variant := strings.TrimSpace(mapping.Variant)
		canonical := strings.ToLower(strings.TrimSpace(mapping.Canonical))
		if variant == "" || canonical == "" {
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: "error", Kind: "empty_mapping", Message: "explicit mapping needs a variant and canonical name", Index: index + 1, Variant: variant, Canonical: canonical,
			})
			continue
		}
		if strings.ContainsAny(canonical, "/[]") {
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: "error", Kind: "path_canonical", Message: "canonical name must not contain a path or channel label", Index: index + 1, Variant: variant, Canonical: canonical,
			})
			continue
		}
		if normalizeWithoutExplicit(canonical, rules) != canonical {
			diagnostics = append(diagnostics, RuleDiagnostic{
				Severity: "error", Kind: "unstable_canonical", Message: "canonical name is changed by the active normalization rules", Index: index + 1, Variant: variant, Canonical: canonical,
			})
		}
	}
	mappingsByKey := make(map[string]string, len(rules.ExplicitMappings)*2)
	for index, mapping := range rules.ExplicitMappings {
		canonical := strings.ToLower(strings.TrimSpace(mapping.Canonical))
		if canonical == "" {
			continue
		}
		for _, key := range []string{
			dotDashKey(normalizeToRouterBase(mapping.Variant, rules)),
			dotDashKey(normalizeToBase(mapping.Variant, rules)),
		} {
			if key == "" {
				continue
			}
			if previous, ok := mappingsByKey[key]; ok && previous != canonical {
				diagnostics = append(diagnostics, RuleDiagnostic{
					Severity:  "error",
					Kind:      "mapping_conflict",
					Message:   fmt.Sprintf("normalized variant %q maps to both %q and %q", key, previous, canonical),
					Index:     index + 1,
					Variant:   strings.TrimSpace(mapping.Variant),
					Canonical: canonical,
				})
				continue
			}
			mappingsByKey[key] = canonical
		}
	}
	return diagnostics
}

func normalizeWithoutExplicit(name string, rules Rules) string {
	result := normalizeToRouterBase(name, rules)
	result = stripFunctionalSuffixes(result, rules)
	return strings.ToLower(strings.TrimSpace(result))
}

func hasPrefixBoundary(value string) bool {
	if value == "" {
		return false
	}
	last := value[len(value)-1]
	switch last {
	case '-', '/', '.', ':', '_', ']', ')':
		return true
	default:
		return false
	}
}

func validateFunctionalSuffix(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("suffix must not be empty")
	}
	if strings.ContainsAny(value, "/\r\n\t") {
		return fmt.Errorf("suffix must not contain a path or control character")
	}
	if strings.Contains(strings.ToLower(value), `\d`) &&
		strings.ContainsAny(strings.ToLower(value), "km") {
		return fmt.Errorf("suffix %q may remove a context-length token", value)
	}
	if isRegexSuffix(value) {
		if _, err := regexp.Compile("(?i)(?:" + value + ")$"); err != nil {
			return fmt.Errorf("suffix contains invalid regular expression: %v", err)
		}
	}
	fragment := suffixSemanticFragment(value)
	for _, token := range strings.FieldsFunc(strings.ToLower(fragment), func(r rune) bool {
		return r == '-' || r == '.' || r == '_' || r == ':' || r == '@' || r == '(' || r == ')' || r == '[' || r == ']'
	}) {
		if _, protected := protectedFunctionalTokens[token]; protected {
			return fmt.Errorf("suffix %q contains protected model semantic token %q", value, token)
		}
	}
	return nil
}

func suffixSemanticFragment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "-")
	if isRegexSuffix(value) {
		// Regex syntax is allowed for dates and provider decorations, but a
		// pattern containing a literal protected token remains unsafe.
		value = strings.ReplaceAll(value, `\d`, "")
		value = strings.ReplaceAll(value, `\w`, "")
		value = strings.ReplaceAll(value, "{", "")
		value = strings.ReplaceAll(value, "}", "")
		value = strings.ReplaceAll(value, "+", "")
		value = strings.ReplaceAll(value, "*", "")
		value = strings.ReplaceAll(value, "?", "")
		value = strings.ReplaceAll(value, "|", "-")
	}
	return value
}

func validateRuleStrings(kind string, values []string, invalid func(string) bool) error {
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("%s #%d must not be empty", kind, index+1)
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate %s %q", kind, trimmed)
		}
		seen[key] = struct{}{}
		if invalid(trimmed) {
			return fmt.Errorf("%s %q must not contain a path", kind, trimmed)
		}
	}
	return nil
}
