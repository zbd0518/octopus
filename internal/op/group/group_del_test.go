package group

import (
	"context"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func TestGroupDelAllRemovesGroupsAndItems(t *testing.T) {
	ctx := initGroupDelTestDB(t)
	created := &model.Group{
		Name:         "delall-target",
		EndpointType: model.EndpointTypeChat,
		Mode:         model.GroupModeRandom,
		Items: []model.GroupItem{
			{ChannelID: 1, ModelName: "delall-model", Priority: 1, Weight: 1},
		},
	}
	if err := GroupCreate(created, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}

	count, err := GroupDelAll(ctx)
	if err != nil {
		t.Fatalf("GroupDelAll: %v", err)
	}
	if count != 1 {
		t.Fatalf("deleted count = %d, want 1", count)
	}
	if groups, err := GroupList(ctx); err != nil || len(groups) != 0 {
		t.Fatalf("groups after delete = %v, err = %v", groups, err)
	}
	var items []model.GroupItem
	if err := db.GetDB().Find(&items).Error; err != nil {
		t.Fatalf("query items: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no items, got %d", len(items))
	}
}

func TestGroupDelRemovesGroupAndItems(t *testing.T) {
	ctx := initGroupDelTestDB(t)
	g := &model.Group{
		Name:         "single-del",
		EndpointType: model.EndpointTypeChat,
		Mode:         model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 2, ModelName: "single-model", Priority: 1, Weight: 1},
		},
	}
	if err := GroupCreate(g, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := GroupDel(g.ID, ctx); err != nil {
		t.Fatalf("GroupDel: %v", err)
	}
	if _, err := GroupGet(g.ID, ctx); err == nil || !strings.Contains(err.Error(), "group not found") {
		t.Fatalf("expected group not found after delete, got %v", err)
	}
	var items []model.GroupItem
	if err := db.GetDB().Where("group_id = ?", g.ID).Find(&items).Error; err != nil {
		t.Fatalf("query items: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no items, got %d", len(items))
	}
}

func initGroupDelTestDB(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	dsn := "file:" + strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()) + "?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := RefreshAllCache(ctx); err != nil {
		t.Fatalf("refresh cache: %v", err)
	}
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
		_ = db.Close()
	})
	return ctx
}
