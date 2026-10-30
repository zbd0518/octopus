package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 56,
		Up:      migratePoolProxyFallback,
	})
}

// 056: pool account proxy fallback column (guide card B4-#13).
// pool_accounts gains proxy_fallback_origin_id (INT NULL):
//   - NULL = not in fallback (the concurrency guard for entering fallback);
//   - non-NULL = the account currently runs on its backup proxy and this
//     holds the original proxy_config_id (sub2api ent/schema/account.go:93-96
//     semantics). The backup proxy itself is a per-account Extra JSON field.
//
// Column-add pattern of 051 (HasTable + HasColumn guards; pool_accounts is
// not part of the main AutoMigrate list, so the explicit migration is
// mandatory).
func migratePoolProxyFallback(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PoolAccount{}) {
		return nil
	}
	if db.Migrator().HasColumn(&model.PoolAccount{}, "ProxyFallbackOriginID") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.PoolAccount{}, "ProxyFallbackOriginID"); err != nil {
		return fmt.Errorf("add column pool_accounts.proxy_fallback_origin_id: %w", err)
	}
	return nil
}
