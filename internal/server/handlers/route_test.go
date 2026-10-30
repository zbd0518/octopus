package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/airoute"
)

func TestGenerateAIRouteConflictReturnsSingleTranslatedError(t *testing.T) {
	setupSettingTestDB(t)
	if err := airoute.AIRouteTaskCreate(context.Background(), model.GenerateAIRouteProgress{
		ID: "active-route-conflict", Scope: model.AIRouteScopeTable,
		CategoryStrategy: model.AIRouteCategoryVendor, Status: model.AIRouteTaskStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/route/ai-generate", strings.NewReader(`{"scope":"table","category_strategy":"channel_tier"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	generateAIRoute(c)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Code       int            `json:"code"`
		MessageKey string         `json:"message_key"`
		Data       map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(recorder.Body)
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body = %s", err, recorder.Body.String())
	}
	if envelope.Code != http.StatusConflict || envelope.MessageKey != "group.aiRoute.categoryStrategy.conflict" || envelope.Data != nil {
		t.Fatalf("envelope = %+v, want only the translated conflict", envelope)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("response has trailing content: second decode = %+v, err = %v", extra, err)
	}
}
