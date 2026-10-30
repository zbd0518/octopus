package rewrite

import (
	"fmt"

	"github.com/dlclark/regexp2"
	appmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

type EffectivePromptRewriteRule struct {
	Pattern     *regexp2.Regexp
	Replacement string
}

type EffectiveConfig struct {
	Profile                             appmodel.RequestRewriteProfile
	FlattenTextBlockArrays              bool
	NilContentAsEmptyString             bool
	EnsureAssistantContentWithToolCalls bool
	ToolRoleStrategy                    appmodel.ToolRoleStrategy
	SystemMessageStrategy               appmodel.SystemMessageStrategy
	ExtraHeaders                        map[string]string
	PromptRules                         []EffectivePromptRewriteRule
}

func Resolve(channelType outbound.OutboundType, cfg *appmodel.RequestRewriteConfig) (*EffectiveConfig, bool, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, false, nil
	}
	if err := cfg.Validate(channelType); err != nil {
		return nil, false, err
	}

	effective := &EffectiveConfig{
		Profile:                             cfg.Profile,
		FlattenTextBlockArrays:              true,
		NilContentAsEmptyString:             true,
		EnsureAssistantContentWithToolCalls: true,
		ToolRoleStrategy:                    appmodel.ToolRoleStrategyKeep,
		SystemMessageStrategy:               appmodel.SystemMessageStrategyKeep,
	}
	for index, rule := range cfg.PromptRules {
		pattern, err := regexp2.Compile(rule.Pattern, regexp2.ECMAScript)
		if err != nil {
			return nil, false, fmt.Errorf("invalid prompt rewrite pattern %d: %w", index+1, err)
		}
		effective.PromptRules = append(effective.PromptRules, EffectivePromptRewriteRule{
			Pattern:     pattern,
			Replacement: rule.Replacement,
		})
	}

	codexProfile := string(appmodel.RequestRewriteProfileCodexHeaders)

	if cfg.Profile == appmodel.RequestRewriteProfilePreserve {
		if cfg.HeaderProfile == codexProfile {
			effective.ExtraHeaders = map[string]string{
				"User-Agent": "Codex Desktop/0.131.0 (Windows 10.0.19045; x86_64) unknown (Codex Desktop; 26.519.21041)",
				"Origin":     "https://chat.openai.com",
				"Referer":    "https://chat.openai.com/",
				"Accept":     "application/json",
			}
		}
		return effective, true, nil
	}

	if cfg.HeaderProfile == codexProfile {
		effective.ExtraHeaders = map[string]string{
			"User-Agent": "Codex Desktop/0.131.0 (Windows 10.0.19045; x86_64) unknown (Codex Desktop; 26.519.21041)",
			"Origin":     "https://chat.openai.com",
			"Referer":    "https://chat.openai.com/",
			"Accept":     "application/json",
		}
	}

	if cfg.ToolRoleStrategy != "" {
		effective.ToolRoleStrategy = cfg.ToolRoleStrategy
	}
	if cfg.SystemMessageStrategy != "" {
		effective.SystemMessageStrategy = cfg.SystemMessageStrategy
	}

	return effective, true, nil
}
