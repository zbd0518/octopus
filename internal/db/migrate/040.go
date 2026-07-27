package migrate

import (
	"encoding/json"
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 40,
		Up:      migrateAccountPool,
	})
}

// 040: 号池（Account Pool）子系统。
// - 创建 account_pools / pool_accounts 表（AutoMigrate 兜底）。
// - channels 表增加 pool_id 列。
// - nav_order / nav_visible 存量行补入 "pool"（保序去重、幂等）。
func migrateAccountPool(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	// 确保表存在（AutoMigrate 通常已创建，这里幂等兜底）。
	if !db.Migrator().HasTable(&model.AccountPool{}) {
		if err := db.Migrator().CreateTable(&model.AccountPool{}); err != nil {
			return fmt.Errorf("create account_pools: %w", err)
		}
	}
	if !db.Migrator().HasTable(&model.PoolAccount{}) {
		if err := db.Migrator().CreateTable(&model.PoolAccount{}); err != nil {
			return fmt.Errorf("create pool_accounts: %w", err)
		}
	}

	// channels 表加 pool_id 列。
	if db.Migrator().HasTable(&model.Channel{}) {
		if !db.Migrator().HasColumn(&model.Channel{}, "PoolID") {
			if err := db.Migrator().AddColumn(&model.Channel{}, "PoolID"); err != nil {
				return fmt.Errorf("add column pool_id: %w", err)
			}
		}
	}

	// nav_order / nav_visible 补入 "pool"。
	appendToNavSetting(db, string(model.SettingKeyNavOrder), "pool", "channel")
	appendToNavSetting(db, string(model.SettingKeyNavVisible), "pool", "channel")

	return nil
}

// appendToNavSetting 在 nav JSON 数组中 after 元素之后插入 item（保序去重、幂等）。
func appendToNavSetting(db *gorm.DB, key, item, after string) {
	var setting model.Setting
	if err := db.Where("`key` = ?", key).First(&setting).Error; err != nil {
		return
	}
	var items []string
	if err := json.Unmarshal([]byte(setting.Value), &items); err != nil {
		return
	}
	// 已存在则跳过。
	for _, v := range items {
		if v == item {
			return
		}
	}
	// 在 after 之后插入。
	idx := len(items) // 默认追加到末尾
	for i, v := range items {
		if v == after {
			idx = i + 1
			break
		}
	}
	newItems := make([]string, 0, len(items)+1)
	newItems = append(newItems, items[:idx]...)
	newItems = append(newItems, item)
	newItems = append(newItems, items[idx:]...)
	data, err := json.Marshal(newItems)
	if err != nil {
		return
	}
	db.Model(&setting).Update("value", string(data))
}
