package model

import "testing"

// B3-#5: the gemini OAuth ChannelKey must pass extra.OAuthType through to the
// outbound credential JSON. An empty extra.OAuthType falls back to code_assist
// so existing accounts keep their historical behavior; ai_studio and
// google_one are accepted and stored (google_one is never hard-rejected —
// outbound falls back to code-assist behavior).
func TestEffectiveKeyWithExtra_GeminiOAuthTypePassthrough(t *testing.T) {
	cred := PoolCredential{
		Type:        PoolTypeOAuth,
		AccessToken: "ya29.token",
	}

	cases := []struct {
		name      string
		oauthType string
		projectID string
		want      string
	}{
		{
			name:      "empty oauth type falls back to code_assist",
			oauthType: "",
			projectID: "proj-1",
			want:      `{"access_token":"ya29.token","oauth_type":"code_assist","project_id":"proj-1"}`,
		},
		{
			name:      "code_assist preserved",
			oauthType: OAuthTypeCodeAssist,
			projectID: "proj-1",
			want:      `{"access_token":"ya29.token","oauth_type":"code_assist","project_id":"proj-1"}`,
		},
		{
			name:      "ai_studio passthrough without project",
			oauthType: OAuthTypeAIStudio,
			want:      `{"access_token":"ya29.token","oauth_type":"ai_studio","project_id":""}`,
		},
		{
			name:      "google_one accepted and stored",
			oauthType: OAuthTypeGoogleOne,
			projectID: "proj-2",
			want:      `{"access_token":"ya29.token","oauth_type":"google_one","project_id":"proj-2"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extra := PoolAccountExtra{OAuthType: tc.oauthType, ProjectID: tc.projectID}
			got := cred.EffectiveKeyWithExtra(PoolPlatformGemini, extra)
			if got != tc.want {
				t.Fatalf("EffectiveKeyWithExtra = %s, want %s", got, tc.want)
			}
		})
	}
}

// Non-gemini platforms are unaffected by the gemini oauth_type passthrough.
func TestEffectiveKeyWithExtra_OtherPlatformsUnchanged(t *testing.T) {
	cred := PoolCredential{Type: PoolTypeAPIKey, APIKey: "sk-abc"}
	if got := cred.EffectiveKeyWithExtra(PoolPlatformAnthropic, PoolAccountExtra{OAuthType: OAuthTypeAIStudio}); got != "sk-abc" {
		t.Fatalf("apikey key = %q, want sk-abc", got)
	}
	credOAuth := PoolCredential{Type: PoolTypeOAuth, AccessToken: "tok"}
	if got := credOAuth.EffectiveKeyWithExtra(PoolPlatformGrok, PoolAccountExtra{}); got != "tok" {
		t.Fatalf("grok oauth key = %q, want tok", got)
	}
}
