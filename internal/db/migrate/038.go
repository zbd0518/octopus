package migrate

import (
	"fmt"
	"strings"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 38,
		Up:      migrateStatsMetricsLatencyColumns,
	})
}

// GORM 默认蛇形化产生的错误直方图列名 → 与 upsert/Redis/前端一致的正确列名。
var statsHistogramColumnRenames = [][2]string{
	{"histogram_lt100", "histogram_lt_100"},
	{"histogram100to500", "histogram_100_500"},
	{"histogram500to1k", "histogram_500_1k"},
	{"histogram1kto5k", "histogram_1k_5k"},
	{"histogram_gt5k", "histogram_gt_5k"},
}

// 038: 修复统计表 latency / FTUT / histogram 列缺失与直方图列名不一致。
//
// 背景：
// 1. migration 010 只给 stats_hourlies 加过 latency/FTUT/histogram；
//    daily 维度表（027）及 totals 等在存量库上可能仍缺这些列。
// 2. GORM 默认把 HistogramLt100 映射为 histogram_lt100，而
//    DailyDimension*Update 的 ON CONFLICT 硬编码 histogram_lt_100，
//    Redis/前端也使用带数字下划线的名字 → MySQL Error 1054。
//
// 步骤（幂等）：
// 1. 若仅有错误列名：RENAME 为正确列名
// 2. 若错误与正确列并存：把错误列数据并入正确列后 DROP 错误列
// 3. 确保所有 StatsMetrics latency/FTUT/histogram 列存在（AddColumn）
func migrateStatsMetricsLatencyColumns(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	latencyFields := []string{
		"LatencyP50", "LatencyP95", "LatencyP99",
		"FtutAvg", "FtutP50", "FtutP95", "FtutP99",
		"HistogramLt100", "Histogram100to500", "Histogram500to1k",
		"Histogram1kto5k", "HistogramGt5k",
	}

	tables := []any{
		&model.StatsTotal{},
		&model.StatsDaily{},
		&model.StatsHourly{},
		&model.StatsModel{},
		&model.StatsChannel{},
		&model.StatsAPIKey{},
		&model.StatsDailyChannel{},
		&model.StatsDailyModel{},
		&model.StatsDailyAPIKey{},
		&model.StatsDailyChannelModel{},
		&model.StatsSiteModelHourly{},
	}

	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		tableName := tableNameOf(db, table)
		if err := fixStatsHistogramColumnNames(db, tableName); err != nil {
			return err
		}
		for _, field := range latencyFields {
			if db.Migrator().HasColumn(table, field) {
				continue
			}
			if err := db.Migrator().AddColumn(table, field); err != nil {
				return fmt.Errorf("add column %s on %s: %w", field, tableName, err)
			}
		}
	}
	return nil
}

func tableNameOf(db *gorm.DB, model any) string {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err == nil && stmt.Schema != nil {
		return stmt.Schema.Table
	}
	return db.NamingStrategy.TableName(fmt.Sprintf("%T", model))
}

func fixStatsHistogramColumnNames(db *gorm.DB, tableName string) error {
	existing, err := physicalColumnSet(db, tableName)
	if err != nil {
		return err
	}
	for _, pair := range statsHistogramColumnRenames {
		wrong, correct := pair[0], pair[1]
		hasWrong := existing[wrong]
		hasCorrect := existing[correct]
		switch {
		case hasWrong && !hasCorrect:
			if err := renamePhysicalColumn(db, tableName, wrong, correct); err != nil {
				return err
			}
			delete(existing, wrong)
			existing[correct] = true
		case hasWrong && hasCorrect:
			if err := mergeAndDropHistogramColumn(db, tableName, wrong, correct); err != nil {
				return err
			}
			delete(existing, wrong)
		}
	}
	return nil
}

func physicalColumnSet(db *gorm.DB, tableName string) (map[string]bool, error) {
	cols, err := db.Migrator().ColumnTypes(tableName)
	if err != nil {
		return nil, fmt.Errorf("list columns of %s: %w", tableName, err)
	}
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		set[c.Name()] = true
	}
	return set, nil
}

func renamePhysicalColumn(db *gorm.DB, table, oldName, newName string) error {
	var sql string
	switch db.Dialector.Name() {
	case "mysql":
		// MySQL 5.7 无 RENAME COLUMN，用 CHANGE 并声明为 bigint。
		sql = fmt.Sprintf("ALTER TABLE `%s` CHANGE `%s` `%s` bigint NULL", table, oldName, newName)
	case "postgres", "postgresql":
		sql = fmt.Sprintf(`ALTER TABLE "%s" RENAME COLUMN "%s" TO "%s"`, table, oldName, newName)
	default: // sqlite
		sql = fmt.Sprintf(`ALTER TABLE "%s" RENAME COLUMN "%s" TO "%s"`, table, oldName, newName)
	}
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("rename %s.%s -> %s: %w", table, oldName, newName, err)
	}
	return nil
}

func mergeAndDropHistogramColumn(db *gorm.DB, table, wrong, correct string) error {
	// 将错误列上的非零值并入正确列（正确列优先保留自身非零值）。
	var updateSQL, dropSQL string
	switch db.Dialector.Name() {
	case "mysql":
		updateSQL = fmt.Sprintf(
			"UPDATE `%s` SET `%s` = IF(IFNULL(`%s`,0)=0, IFNULL(`%s`,0), `%s`)",
			table, correct, correct, wrong, correct,
		)
		dropSQL = fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", table, wrong)
	case "postgres", "postgresql":
		updateSQL = fmt.Sprintf(
			`UPDATE "%s" SET "%s" = CASE WHEN COALESCE("%s",0)=0 THEN COALESCE("%s",0) ELSE "%s" END`,
			table, correct, correct, wrong, correct,
		)
		dropSQL = fmt.Sprintf(`ALTER TABLE "%s" DROP COLUMN "%s"`, table, wrong)
	default: // sqlite
		updateSQL = fmt.Sprintf(
			`UPDATE "%s" SET "%s" = CASE WHEN IFNULL("%s",0)=0 THEN IFNULL("%s",0) ELSE "%s" END`,
			table, correct, correct, wrong, correct,
		)
		dropSQL = fmt.Sprintf(`ALTER TABLE "%s" DROP COLUMN "%s"`, table, wrong)
	}
	if err := db.Exec(updateSQL).Error; err != nil {
		return fmt.Errorf("merge %s.%s into %s: %w", table, wrong, correct, err)
	}
	if err := db.Exec(dropSQL).Error; err != nil {
		// SQLite 旧版本可能不支持 DROP COLUMN；忽略并保留孤儿列。
		if db.Dialector.Name() == "sqlite" && strings.Contains(strings.ToLower(err.Error()), "drop column") {
			return nil
		}
		return fmt.Errorf("drop %s.%s: %w", table, wrong, err)
	}
	return nil
}
