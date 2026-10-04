package poolscheduler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

// ratelimit_headers.go — B1-#1: derive the 429 rate-limit reset time from upstream
// response headers instead of a hardcoded cooldown.
// aligned with sub2api ratelimit_service.go:1360 (calculateOpenAI429ResetTime),
// :2427 (parseRetryAfterResetTime), :1452/:1545 (anthropic unified reset, 366d clamp).

// maxResetHorizon clamps any parsed reset timestamp to at most 366 days ahead
// (same clamp口径 as sub2api's anthropic unified reset parsing).
const maxResetHorizon = 366 * 24 * time.Hour

// Compute429Reset derives the rate-limit reset instant for a 429 response from
// upstream headers. ok=false means no trustworthy reset evidence was found; the
// caller then falls back to the pool-level base cooldown.
//
// Resolution order (malformed values fall through the chain):
//  1. OpenAI x-codex-* window headers — only when the exhausted window carries a
//     reset-after value (a non-exhausted 429 is transient and must NOT inject a
//     long cooldown; aligned with sub2api's set-if-exhausted-else-nil policy).
//  2. Generic Retry-After: float seconds or HTTP-date.
//  3. anthropic-ratelimit-unified-reset: unix seconds/milliseconds or RFC3339,
//     clamped to ≤366 days from now.
func Compute429Reset(platform string, headers http.Header, now time.Time) (time.Time, bool) {
	if headers == nil {
		return time.Time{}, false
	}
	if platform == model.PoolPlatformOpenAI {
		if resetAt, ok := computeOpenAI429Reset(headers, now); ok {
			return resetAt, true
		}
	}
	if resetAt, ok := computeRetryAfterReset(headers, now); ok {
		return resetAt, true
	}
	return computeUnifiedReset(headers.Get("anthropic-ratelimit-unified-reset"), now)
}

// computeOpenAI429Reset parses the x-codex-* header family. A reset is only
// trusted when the corresponding window is exhausted (used_percent >= 100);
// otherwise the 429 is transient and no header-derived cooldown is applied.
func computeOpenAI429Reset(headers http.Header, now time.Time) (time.Time, bool) {
	primaryUsed := parseFloatHeader(headers.Get("x-codex-primary-used-percent"))
	primaryReset := parseIntHeader(headers.Get("x-codex-primary-reset-after-seconds"))
	secondaryUsed := parseFloatHeader(headers.Get("x-codex-secondary-used-percent"))
	secondaryReset := parseIntHeader(headers.Get("x-codex-secondary-reset-after-seconds"))

	isPrimaryExhausted := primaryUsed != nil && *primaryUsed >= 100
	isSecondaryExhausted := secondaryUsed != nil && *secondaryUsed >= 100

	if isPrimaryExhausted && primaryReset != nil && *primaryReset > 0 {
		return normalizeResetAt(now.Add(time.Duration(*primaryReset)*time.Second), now)
	}
	if isSecondaryExhausted && secondaryReset != nil && *secondaryReset > 0 {
		return normalizeResetAt(now.Add(time.Duration(*secondaryReset)*time.Second), now)
	}
	return time.Time{}, false
}

// computeRetryAfterReset parses the generic Retry-After header: float seconds
// first, then HTTP-date.
func computeRetryAfterReset(headers http.Header, now time.Time) (time.Time, bool) {
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return time.Time{}, false
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		if seconds > 0 {
			return normalizeResetAt(now.Add(time.Duration(seconds*float64(time.Second))), now)
		}
		return time.Time{}, false
	}
	if parsed, err := http.ParseTime(raw); err == nil {
		return normalizeResetAt(parsed, now)
	}
	return time.Time{}, false
}

// computeUnifiedReset parses the anthropic-ratelimit-unified-reset header value:
// unix seconds/milliseconds (sub2api shape) or RFC3339 (documented Anthropic
// shape). Result is clamped to ≤366 days from now.
func computeUnifiedReset(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if ts, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if ts > 1e11 {
			ts /= 1000 // milliseconds → seconds
		}
		return normalizeResetAt(time.Unix(ts, 0), now)
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return normalizeResetAt(parsed, now)
	}
	return time.Time{}, false
}

// normalizeResetAt validates a parsed reset instant: it must be in the future
// and at most maxResetHorizon away (larger values are clamped).
func normalizeResetAt(resetAt, now time.Time) (time.Time, bool) {
	if !resetAt.After(now) {
		return time.Time{}, false
	}
	if max := now.Add(maxResetHorizon); resetAt.After(max) {
		return max, true
	}
	return resetAt, true
}

func parseFloatHeader(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return &f
	}
	return nil
}

func parseIntHeader(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if i, err := strconv.Atoi(raw); err == nil {
		return &i
	}
	return nil
}
