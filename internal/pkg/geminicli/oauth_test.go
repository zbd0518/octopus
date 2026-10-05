package geminicli

import (
	"testing"
	"time"
)

// B3-#5: the built-in client path must resolve the client secret through the
// three-level fallback chain (caller-supplied cfg.ClientSecret -> environment
// -> built-in public credential) and must never fail on a missing secret.
func TestEffectiveOAuthConfig_BuiltinSecretFallback(t *testing.T) {
	origEnv := envGetter
	t.Cleanup(func() { envGetter = origEnv })

	cases := []struct {
		name       string
		cfgSecret  string
		envSecret  string
		wantSecret string
	}{
		{"settings wins over env", "settings-secret", "env-secret", "settings-secret"},
		{"env used when no settings", "", "env-secret", "env-secret"},
		{"builtin fallback with zero config", "", "", GeminiCLIOAuthClientSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envGetter = func(string) string { return tc.envSecret }
			cfg, err := EffectiveOAuthConfig(OAuthConfig{ClientSecret: tc.cfgSecret})
			if err != nil {
				t.Fatalf("EffectiveOAuthConfig returned error: %v", err)
			}
			if cfg.ClientID != GeminiCLIOAuthClientID {
				t.Fatalf("ClientID = %q, want built-in %q", cfg.ClientID, GeminiCLIOAuthClientID)
			}
			if cfg.ClientSecret != tc.wantSecret {
				t.Fatalf("ClientSecret = %q, want %q", cfg.ClientSecret, tc.wantSecret)
			}
			if cfg.Scopes != DefaultCodeAssistScopes {
				t.Fatalf("Scopes = %q, want default code assist scopes", cfg.Scopes)
			}
		})
	}
}

// A custom client_id still requires an explicit secret: the built-in public
// credential must never be silently paired with a custom client (sub2api
// policy). This also preserves the historical error semantics.
func TestEffectiveOAuthConfig_CustomClientRequiresSecret(t *testing.T) {
	origEnv := envGetter
	t.Cleanup(func() { envGetter = origEnv })
	envGetter = func(string) string { return "" }

	if _, err := EffectiveOAuthConfig(OAuthConfig{ClientID: "my-client.apps.googleusercontent.com"}); err == nil {
		t.Fatal("custom client_id without secret must fail")
	}

	origScopes := "https://example.com/scope"
	cfg, err := EffectiveOAuthConfig(OAuthConfig{ClientID: "my-client", ClientSecret: "s", Scopes: origScopes})
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig: %v", err)
	}
	if cfg.ClientID != "my-client" || cfg.ClientSecret != "s" || cfg.Scopes != origScopes {
		t.Fatalf("custom config not preserved: %+v", cfg)
	}
}

// GetByState locates a session by its state value (Google callbacks only
// return code+state); expired and unknown states must not match.
func TestSessionStore_GetByState(t *testing.T) {
	store := NewSessionStore()
	t.Cleanup(store.Stop)

	if _, _, ok := store.GetByState(""); ok {
		t.Fatal("empty state must not match")
	}
	if _, _, ok := store.GetByState("missing"); ok {
		t.Fatal("unknown state must not match")
	}

	want := &OAuthSession{State: "state-1", CodeVerifier: "v1", PoolID: 7, CreatedAt: time.Now()}
	store.Set("sess-1", want)
	got, id, ok := store.GetByState("state-1")
	if !ok {
		t.Fatal("GetByState must find the stored session")
	}
	if id != "sess-1" || got.PoolID != 7 || got.CodeVerifier != "v1" {
		t.Fatalf("GetByState = (id=%s, %+v)", id, got)
	}
}
