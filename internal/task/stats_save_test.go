package task

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
)

func TestStatsSaveTaskPersistsSiteModelHourly(t *testing.T) {
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "stats.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	op.InvalidateSiteBindingCache()
	t.Cleanup(op.InvalidateSiteBindingCache)
	site := model.Site{Name: "task-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com"}
	if err := db.GetDB().Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	account := model.SiteAccount{SiteID: site.ID, Name: "task-account"}
	if err := db.GetDB().Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.SiteChannelBinding{SiteID: site.ID, SiteAccountID: account.ID, GroupKey: "default", ChannelID: 91001}
	if err := db.GetDB().Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	op.StatsSiteModelHourlyUpdate(binding.ChannelID, "task-model", model.StatsMetrics{RequestSuccess: 1})
	statsSaveTask()
	statsSaveTask()
	var rows []model.StatsSiteModelHourly
	if err := db.GetDB().WithContext(context.Background()).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RequestSuccess != 1 {
		t.Fatalf("saved rows = %+v, want one successful request", rows)
	}
}

func TestStatsSaveTaskRegisteredForBothDatabasePaths(t *testing.T) {
	source := loadInitSource(t)
	if !strings.Contains(source, "Register(TaskStatsSave, statsSaveInterval, false, statsSaveTask)") ||
		!strings.Contains(source, "statsSaveTask()\n\t\t\t\t\treturn nil") {
		t.Fatal("both direct and SQLite queued stats tasks must invoke statsSaveTask")
	}
}
