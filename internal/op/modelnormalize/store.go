package modelnormalize

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
)

// RuleSet is the complete persisted model-normalization configuration.
type RuleSet struct {
	RouterPrefixes      []string
	FunctionalSuffixes  []string
	ExplicitMappings    []ExplicitMapping
	MarketDedupeDefault bool
}

// SaveRuleSet persists all normalization settings atomically and refreshes the
// in-memory setting cache only after the transaction commits.
func SaveRuleSet(ctx context.Context, rules RuleSet) error {
	if err := ValidateRules(Rules{
		RouterPrefixes:     rules.RouterPrefixes,
		FunctionalSuffixes: rules.FunctionalSuffixes,
		ExplicitMappings:   rules.ExplicitMappings,
	}); err != nil {
		return err
	}

	prefixes, err := json.Marshal(rules.RouterPrefixes)
	if err != nil {
		return fmt.Errorf("encode router prefixes: %w", err)
	}
	suffixes, err := json.Marshal(rules.FunctionalSuffixes)
	if err != nil {
		return fmt.Errorf("encode functional suffixes: %w", err)
	}
	mappings, err := json.Marshal(rules.ExplicitMappings)
	if err != nil {
		return fmt.Errorf("encode explicit mappings: %w", err)
	}

	values := map[model.SettingKey]string{
		model.SettingKeyModelNormalizeRouterPrefixes:      string(prefixes),
		model.SettingKeyModelNormalizeFunctionalSuffixes:  string(suffixes),
		model.SettingKeyModelNormalizeExplicitMappings:    string(mappings),
		model.SettingKeyModelNormalizeMarketDedupeDefault: fmt.Sprintf("%t", rules.MarketDedupeDefault),
	}

	if err := setting.SetStringBatch(ctx, values); err != nil {
		return fmt.Errorf("save model normalization rules: %w", err)
	}
	return nil
}
