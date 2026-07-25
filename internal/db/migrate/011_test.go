package migrate

import (
	"strings"
	"testing"
)

// 011 迁移对合法 JSON 应过滤无效项、去重并补齐缺失的默认项（保序）。
func TestCleanupNavSetting_FiltersDedupesAndAppendsDefaults(t *testing.T) {
	db := newNavSettingsDB(t)
	legacy := `["home","checkin","log","home","ops"]`
	if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "nav_order", legacy).Error; err != nil {
		t.Fatalf("insert nav_order: %v", err)
	}

	if err := migrateNavOrderCleanup(db); err != nil {
		t.Fatalf("migrateNavOrderCleanup: %v", err)
	}

	got := navValue(t, db, "nav_order")
	// "checkin" 已并入 Hub 被过滤；"home" 去重；缺失默认项按 defaultNavOrder 序补齐。
	if !strings.HasPrefix(got, `["home","log","ops","`) {
		t.Fatalf("nav_order = %s, want prefix [\"home\",\"log\",\"ops\",...", got)
	}
	if strings.Contains(got, `"checkin"`) {
		t.Fatalf("nav_order still contains filtered item: %s", got)
	}
	if strings.Count(got, `"home"`) != 1 {
		t.Fatalf("nav_order should dedupe home: %s", got)
	}
	for _, item := range defaultNavOrder {
		if !strings.Contains(got, `"`+item+`"`) {
			t.Fatalf("nav_order missing default item %s: %s", item, got)
		}
	}
}

// 011 迁移遇到非法 JSON 时必须保留原值（用户手工编辑出错的配置不得被静默重置）。
func TestCleanupNavSetting_InvalidJSONPreserved(t *testing.T) {
	db := newNavSettingsDB(t)
	broken := `{"not":"an array"`
	for _, key := range []string{"nav_order", "nav_visible"} {
		if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, key, broken).Error; err != nil {
			t.Fatalf("insert %s: %v", key, err)
		}
	}

	if err := migrateNavOrderCleanup(db); err != nil {
		t.Fatalf("migrateNavOrderCleanup: %v", err)
	}

	for _, key := range []string{"nav_order", "nav_visible"} {
		if got := navValue(t, db, key); got != broken {
			t.Fatalf("%s = %s, want preserved %s", key, got, broken)
		}
	}
}

// 行内容已是规范值时迁移为 no-op；连续两次运行结果稳定（幂等）。
func TestCleanupNavSetting_NoChangeAndIdempotent(t *testing.T) {
	db := newNavSettingsDB(t)
	clean := `["home","hub","channel","group","model","analytics","log","notification","ops","apikey","setting","user"]`
	if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "nav_order", clean).Error; err != nil {
		t.Fatalf("insert nav_order: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := migrateNavOrderCleanup(db); err != nil {
			t.Fatalf("migrateNavOrderCleanup run %d: %v", i, err)
		}
		if got := navValue(t, db, "nav_order"); got != clean {
			t.Fatalf("run %d: nav_order = %s, want unchanged %s", i, got, clean)
		}
	}
}
