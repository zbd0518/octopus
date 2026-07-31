package pooltokenrefresh

import (
	"testing"
	"time"
)

func TestComputeNextBackoff_Progression(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		failureCount int
		want         time.Duration
	}{
		{1, 5 * time.Minute},
		{2, 10 * time.Minute},
		{3, 20 * time.Minute},
		{4, 40 * time.Minute},
		{5, 80 * time.Minute},
		{6, 2 * time.Hour}, // 160m 超上限 → 2h
		{10, 2 * time.Hour},
		{100, 2 * time.Hour},
	}
	for _, tc := range cases {
		gotUnix := computeNextBackoff(tc.failureCount, now)
		gotBackoff := time.Unix(gotUnix, 0).Sub(now)
		if gotBackoff != tc.want {
			t.Fatalf("count=%d: backoff=%v want %v", tc.failureCount, gotBackoff, tc.want)
		}
	}
}

func TestComputeNextBackoff_ZeroFailure(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	// 保护：0 视为 1 → base
	gotUnix := computeNextBackoff(0, now)
	if time.Unix(gotUnix, 0).Sub(now) != 5*time.Minute {
		t.Fatalf("zero failure should map to base backoff")
	}
}
