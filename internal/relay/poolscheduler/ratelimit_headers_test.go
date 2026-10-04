package poolscheduler

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestCompute429Reset_RetryAfterSeconds(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("Retry-After", "120")
	resetAt, ok := Compute429Reset("anthropic", h, now)
	if !ok {
		t.Fatalf("expected reset from Retry-After seconds")
	}
	if got := resetAt.Sub(now); got != 120*time.Second {
		t.Fatalf("reset=%v want 120s", got)
	}
}

func TestCompute429Reset_RetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	want := now.Add(30 * time.Minute)
	h := http.Header{}
	h.Set("Retry-After", want.UTC().Format(http.TimeFormat))
	resetAt, ok := Compute429Reset("custom", h, now)
	if !ok {
		t.Fatalf("expected reset from Retry-After HTTP-date")
	}
	if !resetAt.Equal(want) {
		t.Fatalf("reset=%v want %v", resetAt, want)
	}
}

func TestCompute429Reset_UnifiedResetRFC3339(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	want := now.Add(2 * time.Hour)
	h := http.Header{}
	h.Set("anthropic-ratelimit-unified-reset", want.Format(time.RFC3339))
	resetAt, ok := Compute429Reset("anthropic", h, now)
	if !ok {
		t.Fatalf("expected reset from unified-reset RFC3339")
	}
	if !resetAt.Equal(want) {
		t.Fatalf("reset=%v want %v", resetAt, want)
	}
}

func TestCompute429Reset_UnifiedResetUnixSecondsAndMillis(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	want := now.Add(time.Hour)

	secH := http.Header{}
	secH.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(want.Unix(), 10))
	if resetAt, ok := Compute429Reset("anthropic", secH, now); !ok || !resetAt.Equal(want) {
		t.Fatalf("unix seconds: ok=%v reset=%v want %v", ok, resetAt, want)
	}

	msH := http.Header{}
	msH.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(want.UnixMilli(), 10))
	if resetAt, ok := Compute429Reset("anthropic", msH, now); !ok || !resetAt.Equal(want) {
		t.Fatalf("unix millis: ok=%v reset=%v want %v", ok, resetAt, want)
	}
}

func TestCompute429Reset_CodexPrimaryExhausted(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "100")
	h.Set("x-codex-primary-reset-after-seconds", "3600")
	resetAt, ok := Compute429Reset("openai", h, now)
	if !ok {
		t.Fatalf("expected reset from exhausted 7d codex window")
	}
	if got := resetAt.Sub(now); got != time.Hour {
		t.Fatalf("reset=%v want 1h", got)
	}
}

func TestCompute429Reset_CodexSecondaryExhausted(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("x-codex-secondary-used-percent", "100.0")
	h.Set("x-codex-secondary-reset-after-seconds", "900")
	resetAt, ok := Compute429Reset("openai", h, now)
	if !ok {
		t.Fatalf("expected reset from exhausted 5h codex window")
	}
	if got := resetAt.Sub(now); got != 900*time.Second {
		t.Fatalf("reset=%v want 900s", got)
	}
}

func TestCompute429Reset_CodexNotExhaustedFallsThrough(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	// 未耗尽的瞬时 429：reset-after 只是窗口信息，不能作为冷却证据（对齐 sub2api）。
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "50")
	h.Set("x-codex-primary-reset-after-seconds", "3600")
	if _, ok := Compute429Reset("openai", h, now); ok {
		t.Fatalf("non-exhausted codex window must not produce a reset")
	}
}

func TestCompute429Reset_CodexHeadersIgnoredForOtherPlatforms(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "100")
	h.Set("x-codex-primary-reset-after-seconds", "3600")
	if _, ok := Compute429Reset("anthropic", h, now); ok {
		t.Fatalf("codex headers must be ignored for non-openai platforms")
	}
}

func TestCompute429Reset_MalformedValuesFallThrough(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	// 非法 Retry-After + 非法 unified-reset → false（回落池级基础冷却）。
	h := http.Header{}
	h.Set("Retry-After", "soon")
	h.Set("anthropic-ratelimit-unified-reset", "not-a-time")
	if _, ok := Compute429Reset("anthropic", h, now); ok {
		t.Fatalf("malformed headers must not produce a reset")
	}

	// Retry-After 非法但 unified-reset 合法 → 链式回落成功。
	h2 := http.Header{}
	h2.Set("Retry-After", "soon")
	h2.Set("anthropic-ratelimit-unified-reset", now.Add(time.Minute).Format(time.RFC3339))
	if resetAt, ok := Compute429Reset("anthropic", h2, now); !ok {
		t.Fatalf("malformed Retry-After must fall through to unified-reset")
	} else if got := resetAt.Sub(now); got != time.Minute {
		t.Fatalf("reset=%v want 1m", got)
	}

	// Retry-After 为 0/负数 → 视为无证据。
	h3 := http.Header{}
	h3.Set("Retry-After", "0")
	if _, ok := Compute429Reset("custom", h3, now); ok {
		t.Fatalf("zero Retry-After must not produce a reset")
	}

	// 过去的 HTTP-date → 无证据。
	h4 := http.Header{}
	h4.Set("Retry-After", now.Add(-time.Minute).UTC().Format(http.TimeFormat))
	if _, ok := Compute429Reset("custom", h4, now); ok {
		t.Fatalf("past HTTP-date must not produce a reset")
	}
}

func TestCompute429Reset_ClampToMaxHorizon(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	// Retry-After 4000 万秒（约 462 天）→ 钳制到 now+366d。
	h := http.Header{}
	h.Set("Retry-After", "40000000")
	resetAt, ok := Compute429Reset("custom", h, now)
	if !ok {
		t.Fatalf("expected clamped reset")
	}
	if got := resetAt.Sub(now); got != maxResetHorizon {
		t.Fatalf("reset=%v want clamp %v", got, maxResetHorizon)
	}
}

func TestCompute429Reset_NilHeaders(t *testing.T) {
	if _, ok := Compute429Reset("openai", nil, time.Now()); ok {
		t.Fatalf("nil headers must not produce a reset")
	}
}
