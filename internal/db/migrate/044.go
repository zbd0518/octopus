package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 44,
		Up:      migratePoolAccountColumns,
	})
}

// 044: 号池账号扩展（platform/type/models/quota/token_expires_at/last_used_at/error_message/notes）。
// 存量账号视为自定义 apikey（default 'custom'/'apikey'），凭据 JSON 旧格式由 ParsePoolCredential 兼容读取。
func migratePoolAccountColumns(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PoolAccount{}) {
		return nil
	}

	type colAdd struct {
		field string
		col   string
	}
	// 字段名 -> GORM 列名。AddColumn 接受 struct field 名；用 HasColumn(field) 守卫。
	adds := []colAdd{
		{"Platform", "platform"},
		{"Type", "type"},
		{"Models", "models"},
		{"Quota", "quota"},
		{"TokenExpiresAt", "token_expires_at"},
		{"LastUsedAt", "last_used_at"},
		{"ErrorMessage", "error_message"},
		{"Notes", "notes"},
	}
	for _, c := range adds {
		if !db.Migrator().HasColumn(&model.PoolAccount{}, c.field) {
			if err := db.Migrator().AddColumn(&model.PoolAccount{}, c.field); err != nil {
				return fmt.Errorf("add column %s: %w", c.col, err)
			}
		}
	}

	// 回填存量账号默认值（AddColumn 已按 gorm default tag 设置列默认，但已有 NULL 行需补值）。
	if err := db.Model(&model.PoolAccount{}).
		Where("platform = '' OR platform IS NULL").
		Update("platform", model.PoolPlatformCustom).Error; err != nil {
		return fmt.Errorf("backfill platform: %w", err)
	}
	if err := db.Model(&model.PoolAccount{}).
		Where("type = '' OR type IS NULL").
		Update("type", model.PoolTypeAPIKey).Error; err != nil {
		return fmt.Errorf("backfill type: %w", err)
	}

	return nil
}
