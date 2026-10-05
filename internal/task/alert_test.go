package task

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	porop "github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/op/setting"
	st "github.com/lingyuins/octopus/internal/op/stats"
)

func TestNormalizeAlertNotifyLanguage(t *testing.T) {
	tests := []struct {
		name     string
		language string
		want     string
	}{
		{name: "simplified chinese", language: "zh-Hans", want: "zh-Hans"},
		{name: "traditional chinese", language: "zh-Hant", want: "zh-Hant"},
		{name: "english", language: "en", want: "en"},
		{name: "fallback", language: "ja", want: "en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAlertNotifyLanguage(tt.language); got != tt.want {
				t.Fatalf("normalizeAlertNotifyLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildAlertNotificationMessage(t *testing.T) {
	tests := []struct {
		name     string
		ruleName string
		state    string
		language string
		want     string
	}{
		{name: "simplified firing", ruleName: "CPU", state: alertStateFiring, language: "zh-Hans", want: "告警规则 \"CPU\" 已触发"},
		{name: "simplified resolved", ruleName: "CPU", state: alertStateResolved, language: "zh-Hans", want: "告警规则 \"CPU\" 已恢复"},
		{name: "traditional firing", ruleName: "CPU", state: alertStateFiring, language: "zh-Hant", want: "告警規則 \"CPU\" 已觸發"},
		{name: "traditional resolved", ruleName: "CPU", state: alertStateResolved, language: "zh-Hant", want: "告警規則 \"CPU\" 已恢復"},
		{name: "english default", ruleName: "CPU", state: alertStateFiring, language: "en", want: "Alert 'CPU' is firing"},
		{name: "fallback language", ruleName: "CPU", state: alertStateResolved, language: "ja", want: "Alert 'CPU' is resolved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildAlertNotificationMessage(tt.ruleName, tt.state, tt.language); got != tt.want {
				t.Fatalf("buildAlertNotificationMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluateErrorRateUsesSlidingWindow(t *testing.T) {
	setupAlertEvalDB(t)
	now := time.Now().Unix()

	seedRelayAttempt(t, 1, now-60, 1, 10, "gpt-window", model.AttemptSuccess)
	seedRelayAttempt(t, 2, now-90, 1, 10, "gpt-window", model.AttemptSuccess)
	for i := int64(0); i < 20; i++ {
		seedRelayAttempt(t, 100+i, now-7200-i, 1, 10, "gpt-window", model.AttemptFailed)
	}
	st.ResetCachesForTest(model.StatsTotal{ID: 1, StatsMetrics: model.StatsMetrics{RequestSuccess: 2, RequestFailed: 20}}, model.StatsDaily{}, 0, 0, 0)

	eval := evaluateErrorRate(context.Background(), &model.AlertRule{
		ConditionType: model.AlertConditionErrorRate,
		Threshold:     10,
		WindowSec:     300,
	})

	if eval.Firing {
		t.Fatalf("error-rate alert fired from old cumulative failures; current=%v detail=%q", eval.CurrentValue, eval.Detail)
	}
	if eval.CurrentValue != 0 {
		t.Fatalf("current value = %v, want 0 from recent 0/2 failures", eval.CurrentValue)
	}
}

func TestEvaluateErrorRateScopesChannelAndModel(t *testing.T) {
	setupAlertEvalDB(t)
	now := time.Now().Unix()

	seedRelayAttempt(t, 1, now-60, 1, 10, "gpt-bad", model.AttemptFailed)
	for i := int64(0); i < 9; i++ {
		seedRelayAttempt(t, 10+i, now-60-i, 2, 10, "gpt-good", model.AttemptSuccess)
	}
	st.ResetCachesForTest(model.StatsTotal{ID: 1, StatsMetrics: model.StatsMetrics{RequestSuccess: 9, RequestFailed: 1}}, model.StatsDaily{}, 0, 0, 0)

	eval := evaluateErrorRate(context.Background(), &model.AlertRule{
		ConditionType:  model.AlertConditionErrorRate,
		Threshold:      50,
		WindowSec:      300,
		ScopeChannelID: 1,
		ScopeModelName: "gpt-bad",
	})

	if !eval.Firing {
		t.Fatalf("scoped error-rate alert did not fire; current=%v detail=%q", eval.CurrentValue, eval.Detail)
	}
	if eval.CurrentValue != 100 {
		t.Fatalf("current value = %v, want 100 for scoped 1/1 failure", eval.CurrentValue)
	}
}

func TestEvaluateErrorRateScopesGroupItems(t *testing.T) {
	setupAlertEvalDB(t)
	now := time.Now().Unix()
	group := model.Group{Name: "critical", Mode: model.GroupModeFailover, Items: []model.GroupItem{{ChannelID: 3, ModelName: "gpt-critical", Priority: 1, Weight: 1}}}
	if err := db.GetDB().Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	seedRelayAttempt(t, 1, now-60, 3, 10, "gpt-critical", model.AttemptFailed)
	for i := int64(0); i < 9; i++ {
		seedRelayAttempt(t, 10+i, now-60-i, 4, 10, "gpt-other", model.AttemptSuccess)
	}
	st.ResetCachesForTest(model.StatsTotal{ID: 1, StatsMetrics: model.StatsMetrics{RequestSuccess: 9, RequestFailed: 1}}, model.StatsDaily{}, 0, 0, 0)

	eval := evaluateErrorRate(context.Background(), &model.AlertRule{
		ConditionType: model.AlertConditionErrorRate,
		Threshold:     50,
		WindowSec:     300,
		ScopeGroupID:  group.ID,
	})

	if !eval.Firing {
		t.Fatalf("group-scoped error-rate alert did not fire; current=%v detail=%q", eval.CurrentValue, eval.Detail)
	}
	if eval.CurrentValue != 100 {
		t.Fatalf("current value = %v, want 100 for group scoped 1/1 failure", eval.CurrentValue)
	}
}

func setupAlertEvalDB(t *testing.T) {
	t.Helper()
	st.ClearAllCachesForTest()
	dbPath := filepath.Join(t.TempDir(), "alert-eval.db")
	if err := db.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("init log db: %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	t.Cleanup(func() {
		st.ClearAllCachesForTest()
		_ = db.Close()
	})
}

func seedRelayAttempt(t *testing.T, id int64, ts int64, channelID int, apiKeyID int, modelName string, status model.AttemptStatus) {
	t.Helper()
	logItem := model.RelayLog{
		ID:               id,
		Time:             ts,
		RequestModelName: modelName,
		ActualModelName:  modelName,
		RequestAPIKeyID:  apiKeyID,
		ChannelId:        channelID,
	}
	if status == model.AttemptFailed {
		logItem.Error = "upstream failed"
	}
	if err := db.GetLogDB().Create(&logItem).Error; err != nil {
		t.Fatalf("create relay log: %v", err)
	}
	attempt := model.RelayLogAttempt{
		RelayLogID: id,
		ChannelID:  channelID,
		ModelName:  modelName,
		Status:     string(status),
		Time:       ts,
	}
	if err := db.GetLogDB().Create(&attempt).Error; err != nil {
		t.Fatalf("create relay attempt: %v", err)
	}
}

// TestEvaluatePoolAccountError covers the pool_account_error condition
// (B4-#15, pull-style): firing while any/scoped account is in error, resolve
// once recovered, and scope narrowing to a single account.
func TestEvaluatePoolAccountError(t *testing.T) {
	setupAlertEvalDB(t)

	// Pool tables are created by migrations (040) on the first process-wide
	// InitDB; materialize them defensively so this test is order-independent.
	if !db.GetDB().Migrator().HasTable(&model.AccountPool{}) {
		if err := db.GetDB().AutoMigrate(&model.AccountPool{}, &model.PoolAccount{}); err != nil {
			t.Fatalf("auto migrate pool tables: %v", err)
		}
	}

	pool := &model.AccountPool{Name: fmt.Sprintf("pool-alert-%d", time.Now().UnixNano()), Enabled: true}
	if err := db.GetDB().Create(pool).Error; err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() { _ = db.GetDB().Delete(pool) })
	errored := &model.PoolAccount{PoolID: pool.ID, Name: "acct-error", Platform: model.PoolPlatformOpenAI, Type: model.PoolTypeAPIKey}
	healthy := &model.PoolAccount{PoolID: pool.ID, Name: "acct-ok", Platform: model.PoolPlatformOpenAI, Type: model.PoolTypeAPIKey}
	for _, acct := range []*model.PoolAccount{errored, healthy} {
		if err := db.GetDB().Create(acct).Error; err != nil {
			t.Fatalf("create account %s: %v", acct.Name, err)
		}
		t.Cleanup(func() { _ = db.GetDB().Delete(acct) })
	}
	// SetError persists status="error" (same write path the scheduler uses).
	if err := porop.UpdateAccount(pool.ID, errored.ID, map[string]interface{}{"status": "error"}); err != nil {
		t.Fatalf("set error status: %v", err)
	}

	// Any account in error (scope 0) → firing.
	eval := evaluatePoolAccountError(&model.AlertRule{ConditionType: model.AlertConditionPoolAccountError})
	if !eval.Firing || eval.CurrentValue != 1 {
		t.Fatalf("expected firing with 1 errored account, got firing=%v current=%v (%s)", eval.Firing, eval.CurrentValue, eval.Detail)
	}

	// Scoped to the errored account → firing.
	eval = evaluatePoolAccountError(&model.AlertRule{ConditionType: model.AlertConditionPoolAccountError, ScopePoolAccountID: errored.ID})
	if !eval.Firing {
		t.Fatalf("expected firing for the errored scope, got %s", eval.Detail)
	}

	// Scoped to the healthy account → not firing.
	eval = evaluatePoolAccountError(&model.AlertRule{ConditionType: model.AlertConditionPoolAccountError, ScopePoolAccountID: healthy.ID})
	if eval.Firing {
		t.Fatalf("expected not firing for the healthy scope, got %s", eval.Detail)
	}

	// Recover the account → resolve.
	if err := porop.UpdateAccount(pool.ID, errored.ID, map[string]interface{}{"status": "active"}); err != nil {
		t.Fatalf("recover status: %v", err)
	}
	eval = evaluatePoolAccountError(&model.AlertRule{ConditionType: model.AlertConditionPoolAccountError})
	if eval.Firing {
		t.Fatalf("expected resolve after recovery, got %s", eval.Detail)
	}
}
