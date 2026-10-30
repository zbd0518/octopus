package airoute

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/utils/xurl"
)

func TestJoinAIRouteModelsURL(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    string
	}{
		{baseURL: "https://api.example.com", want: "https://api.example.com/v1/models"},
		{baseURL: "https://api.example.com/v1/", want: "https://api.example.com/v1/models"},
		{baseURL: "https://api.example.com/v1/chat/completions", want: "https://api.example.com/v1/models"},
		{baseURL: "https://api.example.com/openai/v1/models?ignored=true", want: "https://api.example.com/openai/v1/models"},
	} {
		t.Run(tc.baseURL, func(t *testing.T) {
			got, err := joinAIRouteModelsURL(tc.baseURL)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("joinAIRouteModelsURL() = %q, want %q", got, tc.want)
			}
		})
	}

	for _, baseURL := range []string{"file:///tmp/models", "https://user:password@example.com/v1"} {
		if _, err := joinAIRouteModelsURL(baseURL); err == nil {
			t.Fatalf("joinAIRouteModelsURL(%q) should reject unsafe input", baseURL)
		}
	}
}

func TestFetchModels(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"gpt-4o"},{"id":"gpt-4.1"},{"id":"gpt-4o"},{"id":""}]}`)
	}))
	defer server.Close()

	models, err := FetchModels(context.Background(), server.URL+"/v1/chat/completions", " test-key ")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"gpt-4.1", "gpt-4o"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestFetchModelsFollowsRedirectToCurrentHost(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("destination path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"redirected-model"}]}`)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/v1/models", http.StatusFound)
	}))
	defer origin.Close()
	models, err := FetchModels(context.Background(), origin.URL, "test-key")
	if err != nil || !reflect.DeepEqual(models, []string{"redirected-model"}) {
		t.Fatalf("models = %v, err = %v, want response from redirect destination", models, err)
	}
}

func TestFetchModelsRejectsPrivateAndCredentialURLs(t *testing.T) {
	for _, baseURL := range []string{"http://127.0.0.1/v1", "http://localhost:8080/v1", "https://user:secret@example.com/v1"} {
		if _, err := FetchModels(context.Background(), baseURL, "secret"); err == nil {
			t.Fatalf("FetchModels(%q) should reject unsafe URL", baseURL)
		}
	}
}

func TestFetchModelsUpstreamErrorDoesNotExposeResponseOrKey(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream secret", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := FetchModels(context.Background(), server.URL, "do-not-leak")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want upstream status", err)
	}
	for _, secret := range []string{"upstream secret", "do-not-leak"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}
