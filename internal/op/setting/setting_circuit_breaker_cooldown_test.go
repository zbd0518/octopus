package setting

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

// setupSettingTestDB 起一个临时 SQLite 主库并刷新设置缓存。
//
// db.InitDB 是全局单例，本包此前没有测试文件，因此独占一个测试进程，
// 不会与其他包的全局状态冲突。crypto.Init 沿用仓库既有测试约定字符串。
func setupSettingTestDB(t *testing.T) {
	t.Helper()

	crypto.Init("octopus-test-encryption-key")

	dsn := filepath.Join(t.TempDir(), "main.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
}

// 缺陷4 端到端回归：circuit_breaker_cooldown 必须可写。
//
// 未修复时 DefaultSettings() 没有该 key 的 seed 行 → RefreshCache 不会为它建行、
// 也不会写进 settingCache → SetString/SetInt 命中「缓存未命中即拒」分支返回
// `setting not found` → handlers/setting.go 映射成 resp.InternalError = HTTP 500，
// 前端 CircuitBreaker.tsx 的设置项永远保存失败。
//
// 修复只需补一行 seed，RefreshCache 会为存量库自动 CreateInBatches 补行，
// 无需数据库迁移——本测试同时验证了这条「无需迁移」的路径。
func TestSetStringCircuitBreakerCooldownAfterRefreshCache(t *testing.T) {
	setupSettingTestDB(t)

	key := model.SettingKeyCircuitBreakerCooldown

	// seed 行必须已经落库（RefreshCache 的 CreateInBatches 路径）。
	var count int64
	if err := db.GetDB().Model(&model.Setting{}).Where("key = ?", key).Count(&count).Error; err != nil {
		t.Fatalf("count settings rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("DB 中 %q 行数 = %d, want 1（RefreshCache 应自动补行，无需迁移）", key, count)
	}

	// 读：默认值必须是 60，与 balancer/circuit.go 的代码兜底值一致。
	got, err := GetString(key)
	if err != nil {
		t.Fatalf("GetString(%q) = %v，want nil（缓存未命中即拒意味着 seed 缺失）", key, err)
	}
	if got != "60" {
		t.Fatalf("GetString(%q) = %q, want %q", key, got, "60")
	}

	// 写：这是缺陷4 的核心症状点。
	if err := SetString(key, "30"); err != nil {
		t.Fatalf("SetString(%q, \"30\") = %v, want nil", key, err)
	}
	got, err = GetString(key)
	if err != nil {
		t.Fatalf("GetString(%q) after set = %v, want nil", key, err)
	}
	if got != "30" {
		t.Fatalf("GetString(%q) after set = %q, want %q", key, got, "30")
	}

	// 整数写入路径（前端 CircuitBreaker.tsx 走的就是数字输入）。
	if err := SetInt(key, 45); err != nil {
		t.Fatalf("SetInt(%q, 45) = %v, want nil", key, err)
	}
	gotInt, err := GetInt(key)
	if err != nil {
		t.Fatalf("GetInt(%q) = %v, want nil", key, err)
	}
	if gotInt != 45 {
		t.Fatalf("GetInt(%q) = %d, want 45", key, gotInt)
	}

	// 写入必须真的落库，而不只是改内存缓存。
	var persisted model.Setting
	if err := db.GetDB().First(&persisted, "key = ?", key).Error; err != nil {
		t.Fatalf("read persisted setting: %v", err)
	}
	if persisted.Value != "45" {
		t.Fatalf("persisted value = %q, want %q", persisted.Value, "45")
	}
}

// 存量库场景：DB 里已经有一批设置行、但没有 cooldown 行（升级到修复版本的
// 真实部署状态）。RefreshCache 必须补上它，而不是只读已有行。
func TestRefreshCacheBackfillsMissingCircuitBreakerCooldownRow(t *testing.T) {
	crypto.Init("octopus-test-encryption-key")

	dsn := filepath.Join(t.TempDir(), "legacy.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	key := model.SettingKeyCircuitBreakerCooldown

	// 模拟存量库：删掉 cooldown 行（若存在），只留一行别的设置。
	if err := db.GetDB().Where("key = ?", key).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("delete legacy row: %v", err)
	}
	legacy := model.Setting{Key: model.SettingKeyRelayRetryCount, Value: "3"}
	if err := db.GetDB().Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	var before int64
	if err := db.GetDB().Model(&model.Setting{}).Where("key = ?", key).Count(&before).Error; err != nil {
		t.Fatalf("count before: %v", err)
	}
	if before != 0 {
		t.Fatalf("precondition: cooldown rows = %d, want 0", before)
	}

	if err := RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}

	var after int64
	if err := db.GetDB().Model(&model.Setting{}).Where("key = ?", key).Count(&after).Error; err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 1 {
		t.Fatalf("RefreshCache 后 cooldown 行数 = %d, want 1（应自动补行，无需数据库迁移）", after)
	}

	if err := SetString(key, "90"); err != nil {
		t.Fatalf("SetString on a backfilled row = %v, want nil", err)
	}
	got, err := GetString(key)
	if err != nil || got != "90" {
		t.Fatalf("GetString(%q) = (%q, %v), want (\"90\", nil)", key, got, err)
	}
}

// 全部 DefaultSettings 的 key 在 RefreshCache 之后都必须可写——
// 这是「三缺一」类缺陷的通用守卫：任何新增 key 忘记补 seed 都会被它抓到。
func TestEveryDefaultSettingIsWritableAfterRefreshCache(t *testing.T) {
	setupSettingTestDB(t)

	defaults := model.DefaultSettings()
	if len(defaults) == 0 {
		t.Fatal("DefaultSettings() 为空，测试前提不成立")
	}

	seen := make(map[model.SettingKey]bool, len(defaults))
	var missing []model.SettingKey

	for _, s := range defaults {
		if seen[s.Key] {
			t.Errorf("DefaultSettings() 含重复 key %q（会让 CreateInBatches 撞主键）", s.Key)
			continue
		}
		seen[s.Key] = true

		if _, err := GetString(s.Key); err != nil {
			missing = append(missing, s.Key)
			continue
		}
		// 写回同值：SetString 对同值早退，不会真的写库，因此这里只在
		// 缓存命中时验证「不返回 setting not found」。
		if err := SetString(s.Key, s.Value); err != nil && strings.Contains(err.Error(), "setting not found") {
			missing = append(missing, s.Key)
		}
	}

	if len(missing) > 0 {
		t.Fatalf("%d 个 DefaultSettings key 无法读写（缓存未命中）：%v；"+
			"这些 key 的前端设置面板会保存失败并返回 HTTP 500",
			len(missing), fmt.Sprint(missing))
	}
}
