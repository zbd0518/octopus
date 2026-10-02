package sitesync

import (
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
)

func TestProjectAccountInvalidatesCachedMissingBinding(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	op.InvalidateSiteBindingCache()
	t.Cleanup(op.InvalidateSiteBindingCache)
	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil || len(channelIDs) == 0 {
		t.Fatalf("projection = %v, err = %v", channelIDs, err)
	}
	channelID := channelIDs[0]
	if err := db.GetDB().Where("channel_id = ?", channelID).Delete(&model.SiteChannelBinding{}).Error; err != nil {
		t.Fatal(err)
	}
	op.InvalidateSiteBindingCache()
	op.StatsSiteModelHourlyUpdate(channelID, "binding-cache-model", model.StatsMetrics{RequestSuccess: 100})
	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	var binding model.SiteChannelBinding
	if err := db.GetDB().Where("channel_id = ?", channelID).First(&binding).Error; err != nil {
		t.Fatalf("projection did not reuse the channel: %v", err)
	}
	op.StatsSiteModelHourlyUpdate(channelID, "binding-cache-model", model.StatsMetrics{RequestSuccess: 1})
	if err := op.StatsSiteModelHourlySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	var rows []model.StatsSiteModelHourly
	if err := db.GetDB().Where("model_name = ?", "binding-cache-model").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RequestSuccess != 1 {
		t.Fatalf("rows after projection = %+v, want one successful request", rows)
	}
}
