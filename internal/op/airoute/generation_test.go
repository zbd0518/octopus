package airoute

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

func TestGenerateAIRoutesForBucketWithServiceRejectsEmptyRoutes(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("request path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"routes\":[]}"}}]}`)
	}))
	defer server.Close()

	_, err := generateAIRoutesForBucketWithService(
		context.Background(),
		aiRouteService{Name: "test-service", BaseURL: server.URL, APIKey: "test-key", Model: "analyzer"},
		aiRoutePromptBucket{
			PromptEndpointType: model.EndpointTypeChat,
			GroupEndpointType:  model.EndpointTypeChat,
			ModelInputs: []aiRoutePromptModelInput{{
				ChannelID: 1,
				Model:     "gpt-4o",
			}},
		},
		"",
		model.AIRouteCategoryNone,
		1,
		1,
		nil,
	)
	if err == nil {
		t.Fatal("empty routes response returned nil error")
	}

	var i18nErr AIRouteI18nError
	if !errors.As(err, &i18nErr) {
		t.Fatalf("error = %T, want AIRouteI18nError", err)
	}
	if got := i18nErr.I18nMessageKey(); got != I18nKeyAIRouteEmptyResult {
		t.Fatalf("I18nMessageKey() = %q, want %q", got, I18nKeyAIRouteEmptyResult)
	}
}

func TestGenerateAIRoutesForBucketWaitsWhenAllServicesAreRateLimited(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	newRateLimitedServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
		}))
	}

	first := newRateLimitedServer()
	defer first.Close()
	second := newRateLimitedServer()
	defer second.Close()

	pool := newAIRouteServicePool([]aiRouteService{
		{Name: "first", BaseURL: first.URL, APIKey: "key-1", Model: "analyzer"},
		{Name: "second", BaseURL: second.URL, APIKey: "key-2", Model: "analyzer"},
	})
	_, err := generateAIRoutesForBucket(
		context.Background(),
		pool,
		2,
		aiRoutePromptBucket{
			PromptEndpointType: model.EndpointTypeChat,
			GroupEndpointType:  model.EndpointTypeChat,
			ModelInputs: []aiRoutePromptModelInput{{
				ChannelID: 1,
				Model:     "gpt-4o",
			}},
		},
		"",
		model.AIRouteCategoryNone,
		1,
		nil,
	)
	if err == nil {
		t.Fatal("all rate-limited services returned nil error")
	}

	var cooldownErr *aiRouteCooldownError
	if !errors.As(err, &cooldownErr) {
		t.Fatalf("error = %T, want aiRouteCooldownError", err)
	}
	if cooldownErr.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v, want positive cooldown", cooldownErr.RetryAfter)
	}
}
