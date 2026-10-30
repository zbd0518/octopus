package relay

import (
	tm "github.com/lingyuins/octopus/internal/transformer/model"
	"net/http"
	"net/url"
	"testing"
)

func TestCacheFingerprintSeparatesParametersAndBypassesMultimodal(t *testing.T) {
	text := "hello"
	temperatureA, temperatureB := 0.1, 0.9
	a := &tm.InternalLLMRequest{Model: "model", Temperature: &temperatureA, Messages: []tm.Message{{Role: "user", Content: tm.MessageContent{Content: &text}}}}
	b := *a
	b.Temperature = &temperatureB
	ka, ok := cacheRequestFingerprint(a, false)
	if !ok {
		t.Fatal("plain text rejected")
	}
	kb, ok := cacheRequestFingerprint(&b, false)
	if !ok || ka == kb {
		t.Fatal("sampling parameters collided")
	}
	b = *a
	b.Messages = []tm.Message{{Role: "user", Content: tm.MessageContent{MultipleContent: []tm.MessageContentPart{{Type: "text", Text: &text}, {Type: "image_url", ImageURL: &tm.ImageURL{URL: "http://example.invalid/image"}}}}}}
	if _, ok := cacheRequestFingerprint(&b, true); ok {
		t.Fatal("multimodal request accepted")
	}
	b = *a
	b.RawRequest = []byte(`{"model":"model","previous_response_id":"resp_private","input":"hello"}`)
	if _, ok := cacheRequestFingerprint(&b, true); ok {
		t.Fatal("stateful continuation accepted")
	}
}

func TestCacheFingerprintSeparatesQueryAndSharedHeaders(t *testing.T) {
	text := "hello"
	req := &tm.InternalLLMRequest{
		Model:    "model",
		Query:    url.Values{"priority": {"fast"}},
		Messages: []tm.Message{{Role: "user", Content: tm.MessageContent{Content: &text}}},
	}
	otherQuery := *req
	otherQuery.Query = url.Values{"priority": {"slow"}}
	first, ok := cacheRequestFingerprint(req, true)
	if !ok {
		t.Fatal("request with query was rejected")
	}
	second, ok := cacheRequestFingerprint(&otherQuery, true)
	if !ok || first == second {
		t.Fatal("query parameters collided in cache fingerprint")
	}

	headersA := http.Header{"X-Client": {"desktop"}}
	headersB := http.Header{"X-Client": {"mobile"}}
	keyA, ok := requestSingleflightKeyWithHeaders(7, "chat", "model", text, req, headersA)
	if !ok {
		t.Fatal("request with headers was rejected")
	}
	keyB, ok := requestSingleflightKeyWithHeaders(7, "chat", "model", text, req, headersB)
	if !ok || keyA == keyB {
		t.Fatal("shared request headers collided")
	}
}
