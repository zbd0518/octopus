package group

import (
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func TestGroupThinkingModePersistenceAndValidation(t *testing.T) {
	ctx := initGroupCategoryTestDB(t)
	group := &model.Group{Name: t.Name(), EndpointType: "chat", Mode: model.GroupModeRandom, ThinkingMode: " OFF "}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatal(err)
	}
	if group.ThinkingMode != "off" {
		t.Fatalf("create thinking mode = %q", group.ThinkingMode)
	}
	for _, mode := range []string{"on", "auto", "off", ""} {
		updated, err := GroupUpdate(&model.GroupUpdateRequest{ID: group.ID, ThinkingMode: &mode}, ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := model.NormalizeThinkingMode(mode)
		if updated.ThinkingMode != want {
			t.Fatalf("update = %q, want %q", updated.ThinkingMode, want)
		}
		var stored model.Group
		if err := db.GetDB().First(&stored, group.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.ThinkingMode != want {
			t.Fatalf("stored = %q, want %q", stored.ThinkingMode, want)
		}
	}
	invalid := "unsupported"
	if _, err := GroupUpdate(&model.GroupUpdateRequest{ID: group.ID, ThinkingMode: &invalid}, ctx); err == nil {
		t.Fatal("invalid update accepted")
	}
	if err := GroupCreate(&model.Group{Name: "invalid", ThinkingMode: invalid}, ctx); err == nil {
		t.Fatal("invalid create accepted")
	}
	if err := RefreshAllCache(ctx); err != nil {
		t.Fatal(err)
	}
	cached, err := GroupGet(group.ID, ctx)
	if err != nil || cached.ThinkingMode != "auto" {
		t.Fatalf("reloaded mode = %q, err = %v", cached.ThinkingMode, err)
	}
}
