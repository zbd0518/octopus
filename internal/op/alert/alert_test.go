package alert

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func setupAlertTestDB(t *testing.T) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "alert_test.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

func TestRuleDelete_CleansStateCache(t *testing.T) {
	setupAlertTestDB(t)
	ctx := context.Background()

	// 创建告警规则
	rule := model.AlertRule{
		Name:          "test_rule",
		ConditionType: model.AlertConditionChannelDown,
		Enabled:       true,
	}
	if err := db.GetDB().Create(&rule).Error; err != nil {
		t.Fatalf("create rule failed: %v", err)
	}

	// 触发状态缓存写入
	StateSet(rule.ID, model.AlertStateFiring)

	// 验证状态已缓存
	if _, ok := stateCache.Load(rule.ID); !ok {
		t.Fatalf("state not cached after StateSet")
	}

	// 删除规则
	if err := RuleDelete(ctx, rule.ID); err != nil {
		t.Fatalf("RuleDelete failed: %v", err)
	}

	// 验证状态缓存已清理
	if _, ok := stateCache.Load(rule.ID); ok {
		t.Errorf("stateCache still contains deleted rule %d", rule.ID)
	}
}

func TestRuleDelete_NotFound(t *testing.T) {
	setupAlertTestDB(t)
	ctx := context.Background()

	err := RuleDelete(ctx, 99999)
	if err == nil {
		t.Fatal("RuleDelete on non-existent rule should return error")
	}
}

func TestStateGet_CreatesDefaultIfMissing(t *testing.T) {
	// 清空缓存
	stateCache.Range(func(key, value interface{}) bool {
		stateCache.Delete(key)
		return true
	})

	record := StateGet(1)
	if record.State != model.AlertStateOK {
		t.Errorf("default state = %d, want %d", record.State, model.AlertStateOK)
	}
}
