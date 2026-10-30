package stats

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func postgresDialectorOnly() *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{Dialector: postgres.Dialector{}}}
}

func sqliteDialectorOnly() *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{Dialector: sqlite.Dialector{}}}
}

func assignmentSQL(t *testing.T, set clause.Set, column string) string {
	t.Helper()
	for _, item := range set {
		if item.Column.Name != column {
			continue
		}
		expr, ok := item.Value.(clause.Expr)
		if !ok {
			t.Fatalf("column %q value is %T, want clause.Expr", column, item.Value)
		}
		return expr.SQL
	}
	t.Fatalf("column %q not found in assignments", column)
	return ""
}

func TestDailyDimensionUpdateColumnsPostgresQualifiesTarget(t *testing.T) {
	dbConn := postgresDialectorOnly()
	tables := []string{
		"stats_daily_channels",
		"stats_daily_models",
		"stats_daily_api_keys",
		"stats_daily_channel_models",
	}
	accumulateColumns := []string{
		"input_token",
		"output_token",
		"input_cost",
		"output_cost",
		"wait_time",
		"request_success",
		"request_failed",
		"histogram_lt_100",
		"histogram_100_500",
		"histogram_500_1k",
		"histogram_1k_5k",
		"histogram_gt_5k",
	}
	maxColumns := []string{
		"latency_p50",
		"latency_p95",
		"latency_p99",
		"ftut_avg",
		"ftut_p50",
		"ftut_p95",
		"ftut_p99",
	}

	for _, table := range tables {
		set := dailyDimensionUpdateColumns(dbConn, table, false)
		for _, column := range accumulateColumns {
			if got, want := assignmentSQL(t, set, column), table+"."+column+" + excluded."+column; got != want {
				t.Errorf("%s.%s expression = %q, want %q", table, column, got, want)
			}
		}
		for _, column := range maxColumns {
			if got, want := assignmentSQL(t, set, column), "GREATEST("+table+"."+column+", excluded."+column+")"; got != want {
				t.Errorf("%s.%s expression = %q, want %q", table, column, got, want)
			}
		}
	}
}

func TestDailyDimensionUpdateColumnsPostgresHasNoBareTargetReference(t *testing.T) {
	set := dailyDimensionUpdateColumns(postgresDialectorOnly(), "stats_daily_api_keys", false)

	for _, item := range set {
		expr, ok := item.Value.(clause.Expr)
		if !ok {
			continue
		}
		column := item.Column.Name
		if strings.HasPrefix(expr.SQL, column+" + ") {
			t.Errorf("column %q still uses an unqualified target reference: %q", column, expr.SQL)
		}
		if strings.HasPrefix(expr.SQL, "GREATEST("+column+",") {
			t.Errorf("column %q still uses an unqualified target reference: %q", column, expr.SQL)
		}
	}
}

func TestDailyDimensionUpdateColumnsSQLiteKeepsExistingExpressions(t *testing.T) {
	set := dailyDimensionUpdateColumns(sqliteDialectorOnly(), "stats_daily_api_keys", false)

	if got, want := assignmentSQL(t, set, "input_token"), "input_token + excluded.input_token"; got != want {
		t.Errorf("sqlite input_token expression = %q, want %q", got, want)
	}
	if got, want := assignmentSQL(t, set, "latency_p50"), "MAX(latency_p50, excluded.latency_p50)"; got != want {
		t.Errorf("sqlite latency_p50 expression = %q, want %q", got, want)
	}
}

func TestDailyDimensionUpdateColumnsIncludesAPIKeyName(t *testing.T) {
	set := dailyDimensionUpdateColumns(postgresDialectorOnly(), "stats_daily_api_keys", true)

	if got, want := assignmentSQL(t, set, "name"), "excluded.name"; got != want {
		t.Errorf("name expression = %q, want %q", got, want)
	}
}
