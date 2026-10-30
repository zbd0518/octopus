package migrate

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 47,
		Up:      migrateModelNormalizeMarketDedupeDefault,
	})
}

// 047: 模型广场归一化去重默认开启。
// 旧默认 "false"（`model_normalize_market_dedupe_default`）导致模型广场不按
// 归一化名合并变体（如 dmxapi-kimi-k2.5 与 kimi-k2.5 各自成行）。将存量默认值
// 翻转为 "true"；条件限定 value='false'，重复执行安全（幂等），不会覆盖用户
// 后续主动改回的值。
func migrateModelNormalizeMarketDedupeDefault(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("settings") {
		return nil
	}
	// 用 clause.Column 让 gorm 按方言自动 quote 列名（MySQL 反引号 / Postgres 双引号），
	// 避免 `key` 在 Postgres 下因反引号非法导致迁移失败、设置不翻转。
	return db.Table("settings").
		Where(clause.Eq{Column: clause.Column{Name: "key"}, Value: "model_normalize_market_dedupe_default"}).
		Where(clause.Eq{Column: clause.Column{Name: "value"}, Value: "false"}).
		Update("value", "true").Error
}
