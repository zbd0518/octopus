package relay

import (
	"net/http"
	"testing"

	dbmodel "github.com/lingyuins/octopus/internal/model"
)

func TestApplyHeaderOverrides_EligibleApikeyAnthropicAppliesAllowed(t *testing.T) {
	acct := &dbmodel.PoolAccount{
		Platform: dbmodel.PoolPlatformAnthropic,
		Type:     dbmodel.PoolTypeAPIKey,
	}
	acct.SetExtra(dbmodel.PoolAccountExtra{
		HeaderOverridesEnabled: true,
		HeaderOverrides: map[string]string{
			"x-custom-header":  "foo",
			"Authorization":    "Bearer evil",
			"x-codex-trace-id": "should-skip",
			"X-API-Key":        "should-skip",
			"content-length":   "should-skip",
			"x-Another":        "bar",
		},
	})
	ra := &relayAttempt{poolAccount: acct, poolType: dbmodel.PoolTypeAPIKey, poolPlatform: dbmodel.PoolPlatformAnthropic}
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	ra.applyHeaderOverrides(req)

	if got := req.Header.Get("x-custom-header"); got != "foo" {
		t.Fatalf("x-custom-header=%q want foo", got)
	}
	if got := req.Header.Get("x-Another"); got != "bar" {
		t.Fatalf("x-Another=%q want bar", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should not be overridden, got %q", got)
	}
	if got := req.Header.Get("x-codex-trace-id"); got != "" {
		t.Fatalf("x-codex-* should be blocked, got %q", got)
	}
	if got := req.Header.Get("X-API-Key"); got != "" {
		t.Fatalf("X-API-Key should be blocked, got %q", got)
	}
}

func TestApplyHeaderOverrides_IneligiblePlatformSkipped(t *testing.T) {
	acct := &dbmodel.PoolAccount{
		Platform: dbmodel.PoolPlatformGemini,
		Type:     dbmodel.PoolTypeOAuth,
	}
	acct.SetExtra(dbmodel.PoolAccountExtra{
		HeaderOverridesEnabled: true,
		HeaderOverrides:        map[string]string{"x-custom": "value"},
	})
	ra := &relayAttempt{poolAccount: acct, poolType: dbmodel.PoolTypeOAuth, poolPlatform: dbmodel.PoolPlatformGemini}
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	ra.applyHeaderOverrides(req)
	if got := req.Header.Get("x-custom"); got != "" {
		t.Fatalf("gemini+oauth should be ineligible, got %q", got)
	}
}

func TestApplyHeaderOverrides_GrokOAuthEligible(t *testing.T) {
	acct := &dbmodel.PoolAccount{
		Platform: dbmodel.PoolPlatformGrok,
		Type:     dbmodel.PoolTypeOAuth,
	}
	acct.SetExtra(dbmodel.PoolAccountExtra{
		HeaderOverridesEnabled: true,
		HeaderOverrides:        map[string]string{"x-custom": "value"},
	})
	ra := &relayAttempt{poolAccount: acct, poolType: dbmodel.PoolTypeOAuth, poolPlatform: dbmodel.PoolPlatformGrok}
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	ra.applyHeaderOverrides(req)
	if got := req.Header.Get("x-custom"); got != "value" {
		t.Fatalf("grok+oauth should be eligible, got %q", got)
	}
}

func TestApplyHeaderOverrides_DisabledSkipped(t *testing.T) {
	acct := &dbmodel.PoolAccount{
		Platform: dbmodel.PoolPlatformOpenAI,
		Type:     dbmodel.PoolTypeAPIKey,
	}
	acct.SetExtra(dbmodel.PoolAccountExtra{
		HeaderOverridesEnabled: false,
		HeaderOverrides:        map[string]string{"x-custom": "value"},
	})
	ra := &relayAttempt{poolAccount: acct, poolType: dbmodel.PoolTypeAPIKey, poolPlatform: dbmodel.PoolPlatformOpenAI}
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	ra.applyHeaderOverrides(req)
	if got := req.Header.Get("x-custom"); got != "" {
		t.Fatalf("disabled flag should skip, got %q", got)
	}
}

func TestEffectiveKeyWithExtra_PersonalAccessToken(t *testing.T) {
	cred := dbmodel.PoolCredential{
		Type:        dbmodel.PoolTypeOAuth,
		AccessToken: "sk-pat-123",
		AccountID:   "acct-1",
	}
	got := cred.EffectiveKeyWithExtra(dbmodel.PoolPlatformOpenAI, dbmodel.PoolAccountExtra{AuthMode: "personalAccessToken"})
	if got != "sk-pat-123" {
		t.Fatalf("personalAccessToken should return raw access_token, got %q", got)
	}
	// 默认（空 extra）仍透传 OAuth JSON。
	gotJSON := cred.EffectiveKeyWithExtra(dbmodel.PoolPlatformOpenAI, dbmodel.PoolAccountExtra{})
	if gotJSON == "" || gotJSON[0] != '{' {
		t.Fatalf("default should be OAuth JSON, got %q", gotJSON)
	}
}
