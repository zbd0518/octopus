package anthropic

import (
	"testing"

	anthropicModel "github.com/lingyuins/octopus/internal/transformer/inbound/anthropic"
)

func TestGetThinkingBudget_MapsExtendedEfforts(t *testing.T) {
	cases := map[string]int64{
		anthropicModel.EffortLow:    1024,
		anthropicModel.EffortMedium: 8192,
		anthropicModel.EffortHigh:   32768,
		"xhigh":                     65536,
		anthropicModel.EffortMax:    128000,
		"unknown":                   8192,
	}
	for effort, want := range cases {
		got := getThinkingBudget(effort, nil)
		if got == nil {
			t.Fatalf("effort %q: budget is nil", effort)
		}
		if *got != want {
			t.Fatalf("effort %q: budget=%d, want %d", effort, *got, want)
		}
	}
}

func TestGetThinkingBudget_ExplicitBudgetWins(t *testing.T) {
	budget := int64(4242)
	got := getThinkingBudget(anthropicModel.EffortMax, &budget)
	if got == nil || *got != budget {
		t.Fatalf("expected explicit budget %d, got %#v", budget, got)
	}
}
