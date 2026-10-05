package migrate

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

func TestAddGroupThinkingModeLegacyAndIdempotent(t *testing.T) {
	db := openLegacyGroupDB(t, "thinking-mode.db")
	createLegacyGroupsTable(t, db, "")
	seedLegacySharedModelGroup(t, db)
	for range 2 {
		if err := addGroupThinkingMode(db); err != nil {
			t.Fatal(err)
		}
	}
	var group model.Group
	if err := db.First(&group).Error; err != nil {
		t.Fatal(err)
	}
	if group.ThinkingMode != "auto" {
		t.Fatalf("legacy group mode = %q, want auto", group.ThinkingMode)
	}
}
