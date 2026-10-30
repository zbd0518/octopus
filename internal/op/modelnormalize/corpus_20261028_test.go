package modelnormalize

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLatestChannelCorpusHasFullNonEmptyCanonicalCoverage(t *testing.T) {
	raw, err := os.ReadFile("testdata/channel_names_20261028181534.json")
	if err != nil {
		t.Fatalf("read latest channel corpus: %v", err)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		t.Fatalf("parse latest channel corpus: %v", err)
	}
	if len(names) != 2953 {
		t.Fatalf("latest corpus has %d names, want 2953", len(names))
	}

	report := AnalyzeNames(names, Rules{})
	if report.InputCount != len(names) {
		t.Fatalf("report input_count = %d, want %d", report.InputCount, len(names))
	}
	if len(report.Unresolved) != 0 {
		t.Fatalf("unresolved names = %d, want 0; first=%q", len(report.Unresolved), report.Unresolved[0])
	}
	if report.CanonicalCount == 0 {
		t.Fatal("canonical_count = 0")
	}
}
