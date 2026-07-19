package sitesync

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

func TestCompactPersistedSiteModelsKeepsCaseVariants(t *testing.T) {
	items := []model.SiteModel{
		{GroupKey: model.SiteDefaultGroupKey, ModelName: "GLM-5.2", Source: "sync"},
		{GroupKey: model.SiteDefaultGroupKey, ModelName: "glm-5.2", Source: "sync"},
		{GroupKey: model.SiteDefaultGroupKey, ModelName: "GLM-5.2", Source: "sync"}, // exact dup
	}
	got := compactPersistedSiteModels(items)
	if len(got) != 2 {
		t.Fatalf("compact length = %d, want 2 (case variants kept, exact dup dropped), got %+v", len(got), got)
	}
	names := map[string]string{}
	for _, item := range got {
		if item.ModelNameKey == "" {
			t.Fatalf("expected model_name_key filled for %q", item.ModelName)
		}
		names[item.ModelName] = item.ModelNameKey
	}
	if names["GLM-5.2"] == "" || names["glm-5.2"] == "" {
		t.Fatalf("missing variants: %+v", names)
	}
	if names["GLM-5.2"] == names["glm-5.2"] {
		t.Fatalf("case variants must have different keys: %+v", names)
	}
}

func TestBuildSiteModelsFillsModelNameKey(t *testing.T) {
	models := buildSiteModels([]string{"GLM-5.2", "glm-5.2"}, model.SiteDefaultGroupKey, "sync")
	if len(models) != 2 {
		t.Fatalf("buildSiteModels len=%d want 2", len(models))
	}
	for _, item := range models {
		if item.ModelNameKey != model.SiteModelNameKey(item.ModelName) {
			t.Fatalf("model %q key=%q want %q", item.ModelName, item.ModelNameKey, model.SiteModelNameKey(item.ModelName))
		}
	}
}
