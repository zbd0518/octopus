package relay

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIKeyAllowsModel(t *testing.T) {
	tests := []struct {
		name    string
		allowed string
		model   string
		want    bool
	}{
		{"unrestricted", "", "gpt-4o", true},
		{"listed", "gpt-4o,gpt-image-1", "gpt-image-1", true},
		{"trim entries", " gpt-4o , gpt-image-1 ", "gpt-image-1", true},
		{"unlisted", "gpt-4o", "gpt-image-1", false},
		{"exact match", "gpt-4o", "gpt-4o-mini", false},
		{"case sensitive", "gpt-4o", "GPT-4O", false},
		{"whitespace is not unrestricted", " ", "gpt-4o", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiKeyAllowsModel(tt.allowed, tt.model); got != tt.want {
				t.Fatalf("apiKeyAllowsModel(%q, %q) = %v, want %v", tt.allowed, tt.model, got, tt.want)
			}
		})
	}
}

func TestMediaHandlerRejectsUnsupportedModel(t *testing.T) {
	for endpoint, cfg := range mediaEndpointConfigs {
		t.Run(cfg.UpstreamPath, func(t *testing.T) {
			body := bytes.NewBufferString(`{"model":"forbidden-model"}`)
			contentType := "application/json"
			if cfg.MultipartInput {
				body.Reset()
				writer := multipart.NewWriter(body)
				if err := writer.WriteField("model", "forbidden-model"); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				contentType = writer.FormDataContentType()
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, cfg.UpstreamPath, body)
			ctx.Request.Header.Set("Content-Type", contentType)
			ctx.Set("supported_models", "allowed-model")

			MediaHandler(endpoint, ctx)

			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "model not supported") {
				t.Fatalf("response = %d %s, want model whitelist rejection before routing", recorder.Code, recorder.Body.String())
			}
		})
	}
}
