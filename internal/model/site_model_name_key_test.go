package model

import "testing"

func TestSiteModelNameKeyIsCaseSensitive(t *testing.T) {
	upper := SiteModelNameKey("GLM-5.2")
	lower := SiteModelNameKey("glm-5.2")
	if upper == "" || lower == "" {
		t.Fatalf("expected non-empty keys, got upper=%q lower=%q", upper, lower)
	}
	if upper == lower {
		t.Fatalf("expected case-sensitive keys to differ, both=%q", upper)
	}
	if len(upper) != 32 || len(lower) != 32 {
		t.Fatalf("expected md5 hex length 32, got %d and %d", len(upper), len(lower))
	}
	if SiteModelNameKey("  GLM-5.2  ") != upper {
		t.Fatalf("expected TrimSpace before hashing")
	}
	if SiteModelNameKey("") != "" {
		t.Fatalf("expected empty name to yield empty key")
	}
}

func TestSiteModelEnsureModelNameKey(t *testing.T) {
	item := SiteModel{GroupKey: "", ModelName: "  GLM-5.2  "}
	item.EnsureModelNameKey()
	if item.GroupKey != SiteDefaultGroupKey {
		t.Fatalf("group key = %q, want default", item.GroupKey)
	}
	if item.ModelName != "GLM-5.2" {
		t.Fatalf("model name = %q, want trimmed", item.ModelName)
	}
	if item.ModelNameKey != SiteModelNameKey("GLM-5.2") {
		t.Fatalf("model name key = %q, want %q", item.ModelNameKey, SiteModelNameKey("GLM-5.2"))
	}
}

func TestSiteModelIdentityKeyDistinguishesCase(t *testing.T) {
	a := SiteModelIdentityKey("default", "GLM-5.2")
	b := SiteModelIdentityKey("default", "glm-5.2")
	if a == b {
		t.Fatalf("identity keys should differ for case variants")
	}
}
