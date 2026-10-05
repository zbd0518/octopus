package pool

import (
	"fmt"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

func createRuleForTest(t *testing.T, rule *model.PoolUnschedRule) {
	t.Helper()
	if err := CreatePoolUnschedRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	t.Cleanup(func() { _ = DeletePoolUnschedRule(rule.ID) })
}

func statusCodeOf(v int) *int { return &v }

// TestUnschedRuleMatchingCoversStatusAndKeyword walks the matching semantics:
// status-code equality, keyword containment, first-match-by-sort-order, and
// disabled rules being skipped.
func TestUnschedRuleMatchingCoversStatusAndKeyword(t *testing.T) {
	createRuleForTest(t, &model.PoolUnschedRule{
		Name: "rule-529", MatchStatusCode: statusCodeOf(529), DurationMinutes: 30, Enabled: true, SortOrder: 10,
	})
	createRuleForTest(t, &model.PoolUnschedRule{
		Name: "rule-quota", MatchKeyword: "insufficient_quota", DurationMinutes: 60, Enabled: true, SortOrder: 20,
	})
	createRuleForTest(t, &model.PoolUnschedRule{
		Name: "rule-disabled", MatchStatusCode: statusCodeOf(529), DurationMinutes: 1, Enabled: false, SortOrder: 1,
	})

	// Status-code hit.
	rule, ok := FirstMatchingUnschedRule(529, "")
	if !ok || rule.Name != "rule-529" {
		t.Fatalf("529 must match rule-529, got ok=%v rule=%v", ok, rule)
	}
	// Keyword hit on the body snippet.
	rule, ok = FirstMatchingUnschedRule(429, `{"error":"insufficient_quota"}`)
	if !ok || rule.Name != "rule-quota" {
		t.Fatalf("keyword must match rule-quota, got ok=%v rule=%v", ok, rule)
	}
	// Disabled rule never matches even with lower sort_order.
	if _, ok := FirstMatchingUnschedRule(529, ""); !ok {
		t.Fatalf("enabled rule-529 must still match")
	}
	// No match on unrelated code/body.
	if _, ok := FirstMatchingUnschedRule(418, "nothing here"); ok {
		t.Fatalf("418 with unrelated body must not match")
	}
	// Keyword set but empty body does not match.
	if _, ok := FirstMatchingUnschedRule(429, ""); ok {
		t.Fatalf("keyword rule must not match an empty body")
	}
}

// TestUnschedRuleFirstBySortOrder locks in the ordering contract.
func TestUnschedRuleFirstBySortOrder(t *testing.T) {
	createRuleForTest(t, &model.PoolUnschedRule{
		Name: "rule-second", MatchStatusCode: statusCodeOf(503), DurationMinutes: 5, Enabled: true, SortOrder: 2,
	})
	createRuleForTest(t, &model.PoolUnschedRule{
		Name: "rule-first", MatchStatusCode: statusCodeOf(503), DurationMinutes: 7, Enabled: true, SortOrder: 1,
	})
	rule, ok := FirstMatchingUnschedRule(503, "")
	if !ok || rule.Name != "rule-first" {
		t.Fatalf("sort_order 1 must win, got ok=%v rule=%v", ok, rule)
	}
}

// TestUnschedRuleValidation rejects rules without a match condition or with a
// non-positive duration.
func TestUnschedRuleValidation(t *testing.T) {
	if err := ValidatePoolUnschedRule(&model.PoolUnschedRule{DurationMinutes: 5}); err == nil {
		t.Fatalf("rule without match condition must be rejected")
	}
	if err := ValidatePoolUnschedRule(&model.PoolUnschedRule{MatchStatusCode: statusCodeOf(200), DurationMinutes: 5}); err == nil {
		t.Fatalf("status code below 400 must be rejected")
	}
	if err := ValidatePoolUnschedRule(&model.PoolUnschedRule{MatchKeyword: "x", DurationMinutes: 0}); err == nil {
		t.Fatalf("non-positive duration must be rejected")
	}
	if err := ValidatePoolUnschedRule(&model.PoolUnschedRule{MatchStatusCode: statusCodeOf(529), DurationMinutes: 30}); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
}

// TestUnschedRuleCacheInvalidatedOnWrite proves the in-process cache follows
// writes: a rule created after a first read is immediately visible to the
// matcher, and deleting it removes it again.
func TestUnschedRuleCacheInvalidatedOnWrite(t *testing.T) {
	// Warm the cache with a read.
	if _, err := ListPoolUnschedRules(); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	name := fmt.Sprintf("cache-rule-%d", time.Now().UnixNano())
	rule := &model.PoolUnschedRule{Name: name, MatchStatusCode: statusCodeOf(599), DurationMinutes: 9, Enabled: true}
	createRuleForTest(t, rule)

	if _, ok := FirstMatchingUnschedRule(599, ""); !ok {
		t.Fatalf("rule created after cache warm-up must be visible immediately")
	}
	if err := DeletePoolUnschedRule(rule.ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if _, ok := FirstMatchingUnschedRule(599, ""); ok {
		t.Fatalf("deleted rule must disappear from the matcher")
	}
}
