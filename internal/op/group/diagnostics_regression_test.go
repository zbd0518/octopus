package group

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/channel"
)

func TestGroupReadDoesNotMutateCachedMembers(t *testing.T) {
	ctx := initGroupCategoryTestDB(t)
	g := model.Group{ID: 929101, Name: "case-sensitive", Items: []model.GroupItem{{ChannelID: 1, ModelName: " padded ", Weight: 0}}}
	groupCache.Set(g.ID, g)
	got, err := GroupGet(g.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	got.Items[0].ModelName = "changed"
	cached, _ := groupCache.Get(g.ID)
	if cached.Items[0].ModelName != " padded " || cached.Items[0].Weight != 0 {
		t.Fatal("read mutated cached members", cached.Items)
	}
}

func TestGroupInvalidUpdateReleasesConnection(t *testing.T) {
	ctx := initGroupCategoryTestDB(t)
	g := model.Group{Name: "valid", EndpointType: "chat"}
	if err := GroupCreate(&g, ctx); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.GetDB().DB()
	sqlDB.SetMaxOpenConns(1)
	for _, field := range []string{"name", "strategy"} {
		empty, invalid := " ", "invalid"
		req := &model.GroupUpdateRequest{ID: g.ID}
		if field == "name" {
			req.Name = &empty
		} else {
			req.ReasoningBufferStrategy = &invalid
		}
		if _, err := GroupUpdate(req, context.Background()); err == nil {
			t.Fatal("invalid input accepted")
		}
		check, cancel := context.WithTimeout(ctx, time.Second)
		err := sqlDB.PingContext(check)
		cancel()
		if err != nil {
			t.Fatal("transaction retained connection", err)
		}
	}
}

func TestGroupIndexesPublishWithoutReadGap(t *testing.T) {
	ctx := initGroupCategoryTestDB(t)
	channel.GetCache().Set(929110, model.Channel{ID: 929110, Enabled: true})
	defer channel.GetCache().Del(929110)
	g := model.Group{ID: 929111, Name: "available", EndpointType: "chat", MatchRegex: "^alias$", Items: []model.GroupItem{{ChannelID: 929110, ModelName: "upstream", Weight: 1}}}
	groupCache.Set(g.ID, g)
	RebuildIndexes()
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				for _, name := range []string{"available", "alias"} {
					if _, err := GroupGetEnabledMapByEndpoint("chat", name, ctx); err != nil {
						t.Error(err)
					}
				}
			}
		}()
	}
	for range 50 {
		RebuildIndexes()
	}
	workers.Wait()
}
