package task

import (
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

// TestPlanProviderDue 验证到点判断：单个覆盖间隔优先，未刷新过视为到点，
// 未到点不刷新，刚过间隔边界视为到点。
func TestPlanProviderDue(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	tenMinAgo := now.Add(-10 * time.Minute)
	fiveMinAgo := now.Add(-5 * time.Minute)
	oneMinAgo := now.Add(-1 * time.Minute)

	cases := []struct {
		name      string
		p         model.PlanProvider
		globalMin int
		want      bool
	}{
		{
			name:      "never refreshed is due",
			p:         model.PlanProvider{},
			globalMin: 30,
			want:      true,
		},
		{
			name:      "single override smaller than global",
			p:         model.PlanProvider{LastRefresh: &tenMinAgo, RefreshIntervalMin: 5},
			globalMin: 30,
			want:      true,
		},
		{
			name:      "single override not due yet",
			p:         model.PlanProvider{LastRefresh: &oneMinAgo, RefreshIntervalMin: 5},
			globalMin: 30,
			want:      false,
		},
		{
			name:      "follows global default when override is 0",
			p:         model.PlanProvider{LastRefresh: &tenMinAgo, RefreshIntervalMin: 0},
			globalMin: 30,
			want:      false,
		},
		{
			name:      "follows global default and due",
			p:         model.PlanProvider{LastRefresh: &fiveMinAgo, RefreshIntervalMin: 0},
			globalMin: 5,
			want:      true,
		},
		{
			name:      "exactly at interval boundary is due",
			p:         model.PlanProvider{LastRefresh: &tenMinAgo, RefreshIntervalMin: 10},
			globalMin: 30,
			want:      true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := planProviderDue(c.p, c.globalMin, now)
			if got != c.want {
				t.Errorf("planProviderDue() = %v, want %v", got, c.want)
			}
		})
	}
}
