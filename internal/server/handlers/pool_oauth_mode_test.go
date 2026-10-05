package handlers

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/pkg/geminicli"
)

// B3-#5: the initiate mode parameter canonicalizes to a known gemini oauth
// mode; empty falls back to code_assist and unknown explicit values are
// rejected so a typo cannot authorize with the wrong scope set.
func TestNormalizeGeminiModeParam(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		oauthType string
		want      string
		wantErr   bool
	}{
		{"empty defaults to code_assist", "", "", model.OAuthTypeCodeAssist, false},
		{"explicit code_assist", "code_assist", "", model.OAuthTypeCodeAssist, false},
		{"ai_studio via mode", "ai_studio", "", model.OAuthTypeAIStudio, false},
		{"ai_studio via oauth_type fallback", "", "ai_studio", model.OAuthTypeAIStudio, false},
		{"google_one accepted", "google_one", "", model.OAuthTypeGoogleOne, false},
		{"mode wins over oauth_type", "ai_studio", "code_assist", model.OAuthTypeAIStudio, false},
		{"whitespace trimmed", "  ai_studio  ", "", model.OAuthTypeAIStudio, false},
		{"unknown rejected", "bogus", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeGeminiModeParam(tc.mode, tc.oauthType)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got mode %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

// ai_studio authorizes with the AI Studio scope set; the code-assist-shaped
// modes (code_assist, google_one, empty) use the Code Assist scopes.
func TestGeminiScopesForMode(t *testing.T) {
	if got := geminiScopesForMode(model.OAuthTypeAIStudio); got != geminicli.DefaultAIStudioScopes {
		t.Fatalf("ai_studio scopes = %q, want AI Studio scopes", got)
	}
	for _, mode := range []string{"", model.OAuthTypeCodeAssist, model.OAuthTypeGoogleOne} {
		if got := geminiScopesForMode(mode); got != geminicli.DefaultCodeAssistScopes {
			t.Fatalf("mode %q scopes = %q, want Code Assist scopes", mode, got)
		}
	}
}
