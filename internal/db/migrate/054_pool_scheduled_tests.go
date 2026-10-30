package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 54,
		Up:      migratePoolScheduledTest,
	})
}

// 054: pool scheduled connectivity test plans (guide card B4-#11).
//   - Creates the pool_scheduled_tests / pool_scheduled_test_results tables.
//   - The account_pools / pool_accounts table family is not part of the main
//     AutoMigrate list, so the tables are created explicitly with HasTable
//     idempotent guards (040.go pattern).
func migratePoolScheduledTest(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if !db.Migrator().HasTable(&model.PoolScheduledTest{}) {
		if err := db.Migrator().CreateTable(&model.PoolScheduledTest{}); err != nil {
			return fmt.Errorf("create pool_scheduled_tests: %w", err)
		}
	}
	// Composite scan index for the runner (enabled, next_run_at). CreateTable
	// does not reliably create tag-defined indexes, so ensure it explicitly.
	if !db.Migrator().HasIndex(&model.PoolScheduledTest{}, "idx_pool_sched_enabled_next") {
		if err := db.Migrator().CreateIndex(&model.PoolScheduledTest{}, "idx_pool_sched_enabled_next"); err != nil {
			return fmt.Errorf("create index idx_pool_sched_enabled_next: %w", err)
		}
	}

	if !db.Migrator().HasTable(&model.PoolScheduledTestResult{}) {
		if err := db.Migrator().CreateTable(&model.PoolScheduledTestResult{}); err != nil {
			return fmt.Errorf("create pool_scheduled_test_results: %w", err)
		}
	}

	return nil
}
