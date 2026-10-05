package pooltokenrefresh

import (
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/pkg/geminicli"
)

// B3-#5: the gemini OAuth client secret is resolved through a three-level
// fallback chain — settings (pool_gemini_client_secret) > environment
// (GEMINI_CLI_OAUTH_CLIENT_SECRET) > built-in public credential — and the
// built-in level guarantees a non-empty result, so refresh never fails merely
// because no secret was configured.
func TestGeminiClientSecret_FallbackChain(t *testing.T) {
	origSettings := geminiSettingsSecretFunc
	origEnv := envGetter
	t.Cleanup(func() {
		geminiSettingsSecretFunc = origSettings
		envGetter = origEnv
	})

	cases := []struct {
		name          string
		settingsValue string
		envValue      string
		want          string
	}{
		{"settings wins", "settings-secret", "env-secret", "settings-secret"},
		{"env used when settings empty", "", "env-secret", "env-secret"},
		{"builtin used with zero config", "", "", geminicli.GeminiCLIOAuthClientSecret},
		{"empty settings value falls through", "  ", "env-secret", "env-secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The mock mirrors the real helper's contract: the settings value
			// is trimmed before being returned (or degraded to "" on error).
			geminiSettingsSecretFunc = func() string { return strings.TrimSpace(tc.settingsValue) }
			envGetter = func(string) string { return tc.envValue }
			if got := geminiClientSecret(); got != tc.want {
				t.Fatalf("geminiClientSecret() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A settings read failure must degrade to the next fallback level, not fail
// the refresh.
func TestGeminiClientSecret_SettingsErrorFallsBack(t *testing.T) {
	origSettings := geminiSettingsSecretFunc
	origEnv := envGetter
	t.Cleanup(func() {
		geminiSettingsSecretFunc = origSettings
		envGetter = origEnv
	})

	geminiSettingsSecretFunc = func() string {
		return "" // represents a read error degraded to empty
	}
	envGetter = func(string) string { return "env-secret" }
	if got := geminiClientSecret(); got != "env-secret" {
		t.Fatalf("geminiClientSecret() = %q, want env-secret", got)
	}

	envGetter = func(string) string { return "" }
	if got := geminiClientSecret(); got != geminicli.GeminiCLIOAuthClientSecret {
		t.Fatalf("geminiClientSecret() = %q, want built-in secret", got)
	}
}
