package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 51,
		Up:      migrateAPIKeyHashColumn,
	})
}

// 051: api_keys 表增加 api_key_hash 列（SHA-256(明文)，确定性查找）。
//
// API Key 的 api_key 列改为存 AES-GCM 加密密文（enc: 前缀，可逆），
// 重启后可解密恢复明文供显示/复制。加密密文因 nonce 随机不可用于
// WHERE 查询，故另存确定性哈希列 api_key_hash 供认证路径定位记录。
//
// 存量数据处理在 RefreshCache（运行时惰性迁移）中完成：
//   - sk- 明文存量 → 加密化 + 补 hash
//   - 64 hex 哈希存量（哈希化时期写入，明文不可逆）→ 补 hash 列（值为哈希本身），
//     认证仍可用（客户端持明文→sha256 匹配），显示/复制标记需重生
//   - enc: 加密存量 → 补 hash 列（解密后计算）
//
// 幂等：HasColumn 守卫，重复执行安全。
func migrateAPIKeyHashColumn(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.APIKey{}) {
		return nil
	}
	if db.Migrator().HasColumn(&model.APIKey{}, "APIKeyHash") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.APIKey{}, "APIKeyHash"); err != nil {
		return fmt.Errorf("add column api_keys.api_key_hash: %w", err)
	}
	return nil
}
