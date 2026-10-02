package op

import (
	"context"
	"errors"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
)

func TestChannelDeleteCleansGroupCacheAndHooks(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := InitCache(); err != nil {
		t.Fatal(err)
	}
	ch := &model.Channel{Name: "delete-target", Enabled: true}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatal(err)
	}
	g := &model.Group{Name: "delete-group", EndpointType: model.EndpointTypeChat,
		Items: []model.GroupItem{{ChannelID: ch.ID, ModelName: "test", Priority: 1, Weight: 1}}}
	if err := GroupCreate(g, ctx); err != nil {
		t.Fatal(err)
	}
	oldHooks := OnChannelDeletedHooks
	calls := 0
	OnChannelDeletedHooks = append(append([]func(int){}, oldHooks...), func(id int) {
		if id == ch.ID {
			calls++
		}
	})
	t.Cleanup(func() { OnChannelDeletedHooks = oldHooks })
	if err := channel.Delete(ch.ID, ctx); err != nil {
		t.Fatal(err)
	}
	cached, err := GroupGet(g.ID, ctx)
	if err != nil || len(cached.Items) != 0 {
		t.Fatalf("cached group after deletion = %+v, err = %v", cached, err)
	}
	routed, err := GroupGetEnabledMapByEndpoint(model.EndpointTypeChat, g.Name, ctx)
	if err == nil && len(routed.Items) != 0 {
		t.Fatalf("routing index still has deleted channel: %+v", routed.Items)
	}
	if calls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", calls)
	}
	if _, err := channel.Get(ch.ID, ctx); err == nil {
		t.Fatal("deleted channel remains cached")
	}
	t.Cleanup(func() {
		group.GetCache().Clear()
		group.GetNameMap().Clear()
		group.RebuildIndexes()
	})
}

func TestSaveCacheContinuesAfterErrors(t *testing.T) {
	old := cacheSaveFuncs
	t.Cleanup(func() { cacheSaveFuncs = old })
	first := errors.New("first save failed")
	last := errors.New("last save failed")
	calls := 0
	cacheSaveFuncs = []CacheSaveFunc{
		func(context.Context) error { calls++; return first },
		func(context.Context) error { calls++; return nil },
		func(context.Context) error { calls++; return last },
	}
	err := SaveCache()
	if calls != 3 || !errors.Is(err, first) || !errors.Is(err, last) {
		t.Fatalf("calls = %d, err = %v; want all saves and both errors", calls, err)
	}
}

func TestSiteModelHourlySaveRetriesWithoutLosingOrDuplicatingData(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	InvalidateSiteBindingCache()
	siteModelHourlyCacheLock.Lock()
	siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
	siteModelHourlyCacheLock.Unlock()
	t.Cleanup(func() {
		InvalidateSiteBindingCache()
		siteModelHourlyCacheLock.Lock()
		siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
		siteModelHourlyCacheLock.Unlock()
	})
	site, account := createSiteOpTestSiteAccount(t, ctx, "retry-site", "retry-account")
	binding := model.SiteChannelBinding{SiteID: site.ID, SiteAccountID: account.ID, GroupKey: "default", ChannelID: 90003}
	if err := db.GetDB().Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	StatsSiteModelHourlyUpdate(binding.ChannelID, "test-model", model.StatsMetrics{RequestSuccess: 2})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := StatsSiteModelHourlySaveDB(cancelled); err == nil {
		t.Fatal("cancelled flush unexpectedly succeeded")
	}
	StatsSiteModelHourlyUpdate(binding.ChannelID, "test-model", model.StatsMetrics{RequestFailed: 3})
	if err := StatsSiteModelHourlySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	StatsSiteModelHourlyUpdate(binding.ChannelID, "test-model", model.StatsMetrics{RequestSuccess: 1})
	if err := StatsSiteModelHourlySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	if err := StatsSiteModelHourlySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	var rows []model.StatsSiteModelHourly
	if err := db.GetDB().Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RequestSuccess != 3 || rows[0].RequestFailed != 3 || rows[0].LastRequestAt == 0 {
		t.Fatalf("persisted rows = %+v, want one row with 3 successes and 3 failures", rows)
	}
}

func TestSiteBindingInvalidationReplacesNegativeCache(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	InvalidateSiteBindingCache()
	t.Cleanup(InvalidateSiteBindingCache)
	if got, err := lookupChannelSiteBinding(90004); err != nil || got.Found {
		t.Fatalf("initial lookup = %+v, err = %v", got, err)
	}
	site, account := createSiteOpTestSiteAccount(t, ctx, "binding-site", "binding-account")
	binding := model.SiteChannelBinding{SiteID: site.ID, SiteAccountID: account.ID, GroupKey: "default", ChannelID: 90004}
	if err := db.GetDB().Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	InvalidateSiteBindingCache()
	if got, err := lookupChannelSiteBinding(binding.ChannelID); err != nil || !got.Found || got.SiteAccountID != account.ID {
		t.Fatalf("lookup after invalidation = %+v, err = %v", got, err)
	}
}
