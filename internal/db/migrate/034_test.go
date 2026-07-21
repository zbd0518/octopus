package migrate

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newNavSettingsDB 建一张仅含 settings(key,value) 的最小库，模拟存量 settings 行。
func newNavSettingsDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "nav.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`).Error; err != nil {
		t.Fatalf("create settings table: %v", err)
	}
	return db
}

func navValue(t *testing.T, db *gorm.DB, key string) string {
	t.Helper()
	var value string
	if err := db.Raw(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value).Error; err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return value
}

// 存量行含 "alert" 时，迁移应将其替换为 "notification"，保序且无残留 "alert"。
func TestMigrateNavAlertToNotification_RenamesAlert(t *testing.T) {
	db := newNavSettingsDB(t)
	legacy := `["home","hub","channel","group","model","analytics","log","alert","ops","apikey","setting","user"]`
	for _, key := range []string{"nav_order", "nav_visible"} {
		if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, key, legacy).Error; err != nil {
			t.Fatalf("insert %s: %v", key, err)
		}
	}

	if err := migrateNavAlertToNotification(db); err != nil {
		t.Fatalf("migrateNavAlertToNotification: %v", err)
	}

	want := `["home","hub","channel","group","model","analytics","log","notification","ops","apikey","setting","user"]`
	for _, key := range []string{"nav_order", "nav_visible"} {
		if got := navValue(t, db, key); got != want {
			t.Fatalf("%s = %s, want %s", key, got, want)
		}
	}
}

// 行同时含 "alert" 与 "notification" 时，应去重为单个 "notification"（占 alert 原位）。
func TestMigrateNavAlertToNotification_DedupesWhenBothPresent(t *testing.T) {
	db := newNavSettingsDB(t)
	both := `["home","log","alert","notification","ops","setting"]`
	if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "nav_order", both).Error; err != nil {
		t.Fatalf("insert nav_order: %v", err)
	}

	if err := migrateNavAlertToNotification(db); err != nil {
		t.Fatalf("migrateNavAlertToNotification: %v", err)
	}

	want := `["home","log","notification","ops","setting"]`
	if got := navValue(t, db, "nav_order"); got != want {
		t.Fatalf("nav_order = %s, want %s", got, want)
	}
}

// 行不含 "alert" 时迁移为 no-op；连续两次运行结果稳定（幂等）。
func TestMigrateNavAlertToNotification_NoAlertUnchanged(t *testing.T) {
	db := newNavSettingsDB(t)
	clean := `["home","log","notification","ops","setting"]`
	if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "nav_visible", clean).Error; err != nil {
		t.Fatalf("insert nav_visible: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := migrateNavAlertToNotification(db); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if got := navValue(t, db, "nav_visible"); got != clean {
			t.Fatalf("run %d nav_visible = %s, want unchanged %s", i, got, clean)
		}
	}
}

// settings 表存在但无相关行时，迁移不应报错。
func TestMigrateNavAlertToNotification_MissingRowsNoError(t *testing.T) {
	db := newNavSettingsDB(t)
	if err := migrateNavAlertToNotification(db); err != nil {
		t.Fatalf("migrateNavAlertToNotification on empty table: %v", err)
	}
}

// settings 表不存在时（极早期库），迁移应安全跳过。
func TestMigrateNavAlertToNotification_NoSettingsTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nosettings.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	if err := migrateNavAlertToNotification(db); err != nil {
		t.Fatalf("migrateNavAlertToNotification without settings table: %v", err)
	}
}

// renameNavItem 单元用例：覆盖替换、去重、无变更、空输入。
func TestRenameNavItem(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "replace preserves position", in: []string{"a", "alert", "b"}, want: []string{"a", "notification", "b"}},
		{name: "dedupe when both present", in: []string{"alert", "notification"}, want: []string{"notification"}},
		{name: "no alert returns nil", in: []string{"home", "notification"}, want: nil},
		{name: "empty returns nil", in: []string{}, want: nil},
		{name: "duplicate alert collapses", in: []string{"alert", "x", "alert"}, want: []string{"notification", "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renameNavItem(tt.in, "alert", "notification")
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("renameNavItem(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
