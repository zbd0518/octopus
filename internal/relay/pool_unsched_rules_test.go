package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
)

// setupUnschedRuleRelayDB initializes an isolated in-memory DB and creates
// the pool tables directly: migrations 040/054/055 only run on the first
// process-wide InitDB, so a per-test DB must materialize them itself.
func setupUnschedRuleRelayDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.AccountPool{}, &model.PoolAccount{}, &model.PoolUnschedRule{}); err != nil {
		t.Fatalf("auto migrate pool tables: %v", err)
	}
}

// poolTestSeq makes every pool name unique within one test process even when
// the OS clock granularity returns identical nanosecond timestamps twice.
var poolTestSeq int64

func nextPoolTestName(prefix string, t *testing.T) string {
	return fmt.Sprintf("%s-%s-%d", prefix, sanitizePoolTestName(t.Name()), atomic.AddInt64(&poolTestSeq, 1))
}

func unschedRuleTestAccount(t *testing.T, platform string) *model.PoolAccount {
	t.Helper()
	p := &model.AccountPool{Name: nextPoolTestName("rule-relay", t), Enabled: true}
	if err := pool.CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	acct := &model.PoolAccount{
		PoolID:      p.ID,
		Name:        "rule-account",
		Platform:    platform,
		Type:        model.PoolTypeAPIKey,
		Credentials: `{"type":"apikey","api_key":"sk-test"}`,
	}
	if err := pool.CreateAccount(acct); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() { _ = pool.DeletePool(p.ID) })
	return acct
}

func tempUnschedOf(t *testing.T, acct *model.PoolAccount) (until int64, reason string) {
	t.Helper()
	fresh, err := pool.GetAccount(acct.PoolID, acct.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	return fresh.TempUnschedUntil, fresh.TempUnschedReason
}

// sanitizePoolTestName strips characters SQLite forbids in shared in-memory
// DSNs so the test name can be reused as the pool name seed.
func sanitizePoolTestName(name string) string {
	return strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(name)
}

func statusCodePtr(v int) *int { return &v }

// TestHandlePoolAuthErrorRuleMatchesOtherCodes: a 529 failure with a
// configured 529→30min rule temp-unschedules the account for the rule's
// duration and the reason JSON carries the rule id (guide B4-#12 acceptance).
func TestHandlePoolAuthErrorRuleMatchesOtherCodes(t *testing.T) {
	setupUnschedRuleRelayDB(t)
	acct := unschedRuleTestAccount(t, model.PoolPlatformCustom)

	rule := &model.PoolUnschedRule{Name: "rule-529", MatchStatusCode: statusCodePtr(529), DurationMinutes: 30, Enabled: true}
	if err := pool.CreatePoolUnschedRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	t.Cleanup(func() { _ = pool.DeletePoolUnschedRule(rule.ID) })

	before := time.Now()
	handlePoolAuthError(acct, model.PoolTypeAPIKey, 529, "")
	until, reason := tempUnschedOf(t, acct)

	wantLow := before.Add(29 * time.Minute).Unix()
	wantHigh := before.Add(31 * time.Minute).Unix()
	if until < wantLow || until > wantHigh {
		t.Fatalf("temp_unsched_until = %d, want within [%d,%d]", until, wantLow, wantHigh)
	}
	var state tempUnschedState
	if err := json.Unmarshal([]byte(reason), &state); err != nil {
		t.Fatalf("reason JSON: %v (%q)", err, reason)
	}
	if state.Trigger != "rule" || state.RuleID != rule.ID || state.StatusCode != 529 {
		t.Fatalf("reason state = %+v, want trigger=rule rule_id=%d code=529", state, rule.ID)
	}
}

// TestHandlePoolAuthErrorNoRuleKeepsHistoricalBehavior: without a matching
// rule, codes outside 403/401 stay a no-op, and the 403/401 paths keep the
// default 10-minute window (golden semantics).
func TestHandlePoolAuthErrorNoRuleKeepsHistoricalBehavior(t *testing.T) {
	setupUnschedRuleRelayDB(t)
	acct := unschedRuleTestAccount(t, model.PoolPlatformCustom)

	// 529 without any rule: historical no-op.
	handlePoolAuthError(acct, model.PoolTypeAPIKey, 529, "")
	if until, _ := tempUnschedOf(t, acct); until != 0 {
		t.Fatalf("529 without rules must stay a no-op, got until=%d", until)
	}

	// Non-OpenAI 403 without rules: default 10-minute window.
	before := time.Now()
	handlePoolAuthError(acct, model.PoolTypeAPIKey, 403, "")
	until, reason := tempUnschedOf(t, acct)
	if until < before.Add(9*time.Minute).Unix() || until > before.Add(11*time.Minute).Unix() {
		t.Fatalf("403 default window = %d, want ~10min after %d", until, before.Unix())
	}
	var state tempUnschedState
	if err := json.Unmarshal([]byte(reason), &state); err != nil || state.Trigger != "http_403" {
		t.Fatalf("403 reason = %q (%v), want trigger=http_403", reason, err)
	}
}

// TestHandlePoolAuthErrorRuleOverrides403DefaultWindow: with a rule matching
// 403, the rule's duration replaces the default window on the counter path
// (rules run after the counter logic, which still increments).
func TestHandlePoolAuthErrorRuleOverrides403DefaultWindow(t *testing.T) {
	setupUnschedRuleRelayDB(t)
	acct := unschedRuleTestAccount(t, model.PoolPlatformOpenAI)

	rule := &model.PoolUnschedRule{Name: "rule-403", MatchStatusCode: statusCodePtr(403), DurationMinutes: 45, Enabled: true}
	if err := pool.CreatePoolUnschedRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	t.Cleanup(func() { _ = pool.DeletePoolUnschedRule(rule.ID) })

	before := time.Now()
	handlePoolAuthError(acct, model.PoolTypeAPIKey, 403, "")
	until, reason := tempUnschedOf(t, acct)

	if until < before.Add(44*time.Minute).Unix() || until > before.Add(46*time.Minute).Unix() {
		t.Fatalf("rule duration must replace the default window, until=%d", until)
	}
	var state tempUnschedState
	if err := json.Unmarshal([]byte(reason), &state); err != nil || state.RuleID != rule.ID {
		t.Fatalf("reason = %q (%v), want rule_id=%d", reason, err, rule.ID)
	}
	// The 403 counter must still have incremented (counter semantics intact).
	fresh, err := pool.GetAccount(acct.PoolID, acct.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if fresh.AuthErrorCount != 1 {
		t.Fatalf("403 counter must keep counting through the rule path, count=%d", fresh.AuthErrorCount)
	}
}

// TestHandlePoolAuthErrorKeywordRuleMatchesBody: a keyword-only rule matches
// when the failure body snippet contains the keyword.
func TestHandlePoolAuthErrorKeywordRuleMatchesBody(t *testing.T) {
	setupUnschedRuleRelayDB(t)
	acct := unschedRuleTestAccount(t, model.PoolPlatformCustom)

	rule := &model.PoolUnschedRule{Name: "rule-kw", MatchKeyword: "insufficient_quota", DurationMinutes: 120, Enabled: true}
	if err := pool.CreatePoolUnschedRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	t.Cleanup(func() { _ = pool.DeletePoolUnschedRule(rule.ID) })

	before := time.Now()
	handlePoolAuthError(acct, model.PoolTypeAPIKey, 429, `{"error":{"type":"insufficient_quota"}}`)
	until, _ := tempUnschedOf(t, acct)
	if until < before.Add(119*time.Minute).Unix() {
		t.Fatalf("keyword rule must apply its duration, until=%d", until)
	}

	// A fresh account with a non-matching body on a non-403/401 code stays
	// untouched (no rule hit → historical no-op).
	other := unschedRuleTestAccount(t, model.PoolPlatformCustom)
	handlePoolAuthError(other, model.PoolTypeAPIKey, 402, "some other error")
	if untilOther, _ := tempUnschedOf(t, other); untilOther != 0 {
		t.Fatalf("non-matching body must leave the account untouched, until=%d", untilOther)
	}
}
