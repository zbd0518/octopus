package group

import (
	"context"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func TestGroupCategoryNormalizeCreateUpdateListCache(t *testing.T) {
	ctx := initGroupCategoryTestDB(t)

	created := &model.Group{
		Name:         "category-model",
		Category:     " premium ",
		EndpointType: model.EndpointTypeChat,
		Mode:         model.GroupModeRandom,
		Items: []model.GroupItem{
			{ChannelID: 1, ModelName: "category-model", Priority: 1, Weight: 1},
		},
	}
	if err := GroupCreate(created, ctx); err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}

	got, err := GroupGet(created.ID, ctx)
	if err != nil {
		t.Fatalf("GroupGet() error = %v", err)
	}
	if got.Category != "premium" {
		t.Fatalf("created cache category = %q, want premium", got.Category)
	}

	groups, err := GroupList(ctx)
	if err != nil {
		t.Fatalf("GroupList() error = %v", err)
	}
	if len(groups) != 1 || groups[0].Category != "premium" {
		t.Fatalf("listed groups = %+v, want category premium", groups)
	}

	updatedCategory := " budget "
	updated, err := GroupUpdate(&model.GroupUpdateRequest{ID: created.ID, Category: &updatedCategory}, ctx)
	if err != nil {
		t.Fatalf("GroupUpdate() error = %v", err)
	}
	if updated.Category != "budget" {
		t.Fatalf("updated category = %q, want budget", updated.Category)
	}

	var stored model.Group
	if err := db.GetDB().WithContext(ctx).First(&stored, created.ID).Error; err != nil {
		t.Fatalf("load stored group: %v", err)
	}
	if stored.Category != "budget" {
		t.Fatalf("stored category = %q, want budget", stored.Category)
	}

	strategy := "immediate"
	updated, err = GroupUpdate(&model.GroupUpdateRequest{ID: created.ID, ReasoningBufferStrategy: &strategy}, ctx)
	if err != nil {
		t.Fatalf("GroupUpdate() reasoning strategy error = %v", err)
	}
	if updated.ReasoningBufferStrategy != strategy {
		t.Fatalf("updated reasoning strategy = %q, want %q", updated.ReasoningBufferStrategy, strategy)
	}
	stored = model.Group{}
	if err := db.GetDB().WithContext(ctx).First(&stored, created.ID).Error; err != nil {
		t.Fatalf("reload stored group: %v", err)
	}
	if stored.ReasoningBufferStrategy != strategy {
		t.Fatalf("stored reasoning strategy = %q, want %q", stored.ReasoningBufferStrategy, strategy)
	}
}

func initGroupCategoryTestDB(t *testing.T) context.Context {
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
