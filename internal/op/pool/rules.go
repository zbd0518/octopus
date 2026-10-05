package pool

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

// In-process cache for temp-unsched rules (B4-#12), the same pattern as
// op/alert's rulesCache: rule reads happen on the relay failure path, so the
// (rarely changing) configuration is cached and invalidated on every write.

var (
	unschedRulesMu     sync.RWMutex
	unschedRulesCache  []model.PoolUnschedRule
	unschedRulesCached bool
)

func invalidateUnschedRulesCache() {
	unschedRulesMu.Lock()
	unschedRulesCached = false
	unschedRulesCache = nil
	unschedRulesMu.Unlock()
}

// loadUnschedRules returns all rules ordered by (sort_order, id), serving
// from the cache when warm. The cached slice is copied so callers cannot
// mutate it.
func loadUnschedRules() ([]model.PoolUnschedRule, error) {
	unschedRulesMu.RLock()
	if unschedRulesCached {
		cached := unschedRulesCache
		unschedRulesMu.RUnlock()
		return cached, nil
	}
	unschedRulesMu.RUnlock()

	rules := make([]model.PoolUnschedRule, 0)
	if err := db.GetDB().Order("sort_order, id").Find(&rules).Error; err != nil {
		return nil, err
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].SortOrder != rules[j].SortOrder {
			return rules[i].SortOrder < rules[j].SortOrder
		}
		return rules[i].ID < rules[j].ID
	})

	unschedRulesMu.Lock()
	unschedRulesCache = rules
	unschedRulesCached = true
	unschedRulesMu.Unlock()
	return rules, nil
}

// ListPoolUnschedRules returns every rule (admin UI).
func ListPoolUnschedRules() ([]model.PoolUnschedRule, error) {
	return loadUnschedRules()
}

// FirstMatchingUnschedRule returns the first enabled rule (by sort_order,
// then id) matching the failure: status-code equality when the rule sets a
// code, or a response-body keyword hit when the rule sets a keyword and the
// body snippet is non-empty. ok=false means no rule matched and the caller
// keeps its historical default behavior.
func FirstMatchingUnschedRule(statusCode int, bodySnippet string) (*model.PoolUnschedRule, bool) {
	rules, err := loadUnschedRules()
	if err != nil {
		// A cache/DB failure must not break the failure path: fall through to
		// "no match" (default cooldown semantics).
		return nil, false
	}
	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled {
			continue
		}
		if rule.MatchStatusCode != nil && *rule.MatchStatusCode == statusCode {
			return rule, true
		}
		if rule.MatchKeyword != "" && bodySnippet != "" && strings.Contains(bodySnippet, rule.MatchKeyword) {
			return rule, true
		}
	}
	return nil, false
}

// ValidatePoolUnschedRule enforces the rule shape: a match condition (status
// code in 400-599 or a keyword) and a positive duration.
func ValidatePoolUnschedRule(rule *model.PoolUnschedRule) error {
	if rule == nil {
		return fmt.Errorf("rule is nil")
	}
	if rule.MatchStatusCode == nil && strings.TrimSpace(rule.MatchKeyword) == "" {
		return fmt.Errorf("rule needs a status code or a body keyword")
	}
	if rule.MatchStatusCode != nil && (*rule.MatchStatusCode < 400 || *rule.MatchStatusCode > 599) {
		return fmt.Errorf("match_status_code must be within 400-599")
	}
	if rule.DurationMinutes <= 0 {
		return fmt.Errorf("duration_minutes must be positive")
	}
	return nil
}

// CreatePoolUnschedRule stores a new rule after validation.
func CreatePoolUnschedRule(rule *model.PoolUnschedRule) error {
	if err := ValidatePoolUnschedRule(rule); err != nil {
		return err
	}
	if err := db.GetDB().Create(rule).Error; err != nil {
		return err
	}
	invalidateUnschedRulesCache()
	return nil
}

// UpdatePoolUnschedRule saves the full rule shape after validation.
func UpdatePoolUnschedRule(rule *model.PoolUnschedRule) error {
	if rule == nil || rule.ID == 0 {
		return fmt.Errorf("rule not found")
	}
	if err := ValidatePoolUnschedRule(rule); err != nil {
		return err
	}
	result := db.GetDB().Model(&model.PoolUnschedRule{}).Where("id = ?", rule.ID).Updates(map[string]interface{}{
		"name":              rule.Name,
		"match_status_code": rule.MatchStatusCode,
		"match_keyword":     rule.MatchKeyword,
		"duration_minutes":  rule.DurationMinutes,
		"enabled":           rule.Enabled,
		"sort_order":        rule.SortOrder,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("rule not found")
	}
	invalidateUnschedRulesCache()
	return nil
}

// DeletePoolUnschedRule removes a rule by id.
func DeletePoolUnschedRule(id int) error {
	result := db.GetDB().Delete(&model.PoolUnschedRule{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("rule not found")
	}
	invalidateUnschedRulesCache()
	return nil
}
