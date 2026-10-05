package gemini

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/pkg/geminicli"
)

// marshalOAuthKey builds an OAuth credential JSON ChannelKey with the given
// oauth_type, mirroring what model.EffectiveKeyWithExtra produces.
func marshalOAuthKey(t *testing.T, oauthType string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"access_token": "ya29.token",
		"oauth_type":   oauthType,
	})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	return string(b)
}

// B3-#5: ai_studio OAuth credentials route to the official Generative
// Language API (/v1beta/models/{model}:{action}) with Bearer auth, no ?key=
// and no Code Assist request wrapper.
func TestTransformRequest_AIStudioCredential(t *testing.T) {
	out := &MessagesOutbound{}
	req, err := out.TransformRequest(context.Background(),
		newLLMRequest("gemini-2.5-pro", false), "", marshalOAuthKey(t, geminicli.OAuthTypeAIStudio))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	if got := req.Header.Get("Authorization"); got != "Bearer ya29.token" {
		t.Fatalf("Authorization = %q, want Bearer ya29.token", got)
	}
	if req.URL.Query().Get("key") != "" {
		t.Fatalf("OAuth request must not carry ?key=: %s", req.URL.String())
	}
	if req.URL.Host != "generativelanguage.googleapis.com" {
		t.Fatalf("host = %q, want generativelanguage.googleapis.com", req.URL.Host)
	}
	if req.URL.Path != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("path = %q, want /v1beta/models/gemini-2.5-pro:generateContent", req.URL.Path)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(body), `"request"`) {
		t.Fatalf("ai_studio body must not be Code Assist wrapped: %s", string(body))
	}
	if !strings.Contains(string(body), `"contents"`) {
		t.Fatalf("ai_studio body missing contents: %s", string(body))
	}
}

// Streaming ai_studio requests use streamGenerateContent with alt=sse.
func TestTransformRequest_AIStudioStream(t *testing.T) {
	out := &MessagesOutbound{}
	req, err := out.TransformRequest(context.Background(),
		newLLMRequest("gemini-2.5-flash", true), "", marshalOAuthKey(t, geminicli.OAuthTypeAIStudio))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if req.URL.Path != "/v1beta/models/gemini-2.5-flash:streamGenerateContent" {
		t.Fatalf("path = %q, want streamGenerateContent path", req.URL.Path)
	}
	if req.URL.Query().Get("alt") != "sse" {
		t.Fatalf("alt = %q, want sse", req.URL.Query().Get("alt"))
	}
}

// A code-assist base URL must be overridden back to the official endpoint for
// ai_studio credentials (the reverse of the code_assist override).
func TestTransformRequest_AIStudioOverridesCodeAssistBaseURL(t *testing.T) {
	out := &MessagesOutbound{}
	req, err := out.TransformRequest(context.Background(),
		newLLMRequest("gemini-2.5-pro", false),
		"https://cloudcode-pa.googleapis.com",
		marshalOAuthKey(t, geminicli.OAuthTypeAIStudio))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if req.URL.Host != "generativelanguage.googleapis.com" {
		t.Fatalf("host = %q, want generativelanguage.googleapis.com", req.URL.Host)
	}
}

// google_one credentials are accepted (not hard-rejected) and fall back to
// the Code Assist outbound shape.
func TestTransformRequest_GoogleOneFallsBackToCodeAssist(t *testing.T) {
	out := &MessagesOutbound{}
	req, err := out.TransformRequest(context.Background(),
		newLLMRequest("gemini-2.5-pro", false), "", marshalOAuthKey(t, geminicli.OAuthTypeGoogleOne))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if req.URL.Host != "cloudcode-pa.googleapis.com" {
		t.Fatalf("host = %q, want cloudcode-pa.googleapis.com", req.URL.Host)
	}
	if req.URL.Path != "/v1internal:generateContent" {
		t.Fatalf("path = %q, want /v1internal:generateContent", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ya29.token" {
		t.Fatalf("Authorization = %q, want Bearer ya29.token", got)
	}
}

// ParseCodeAssistCredential accepts all three known oauth_type values and
// still rejects raw keys and unknown modes.
func TestParseCodeAssistCredential_KnownOAuthTypes(t *testing.T) {
	for _, oauthType := range []string{geminicli.OAuthTypeCodeAssist, geminicli.OAuthTypeAIStudio, geminicli.OAuthTypeGoogleOne} {
		raw, err := json.Marshal(map[string]string{"access_token": "tok", "oauth_type": oauthType})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		cred, ok := geminicli.ParseCodeAssistCredential(string(raw))
		if !ok {
			t.Fatalf("oauth_type %q must be accepted", oauthType)
		}
		if cred.OAuthType != oauthType {
			t.Fatalf("OAuthType = %q, want %q", cred.OAuthType, oauthType)
		}
	}

	if _, ok := geminicli.ParseCodeAssistCredential("AIzaSyPlainKey"); ok {
		t.Fatal("raw API key must not be accepted")
	}
	raw, _ := json.Marshal(map[string]string{"access_token": "tok", "oauth_type": "bogus"})
	if _, ok := geminicli.ParseCodeAssistCredential(string(raw)); ok {
		t.Fatal("unknown oauth_type must not be accepted")
	}
}
