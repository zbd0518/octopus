package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGroupThinkingModeInvalidInputReturnsLocalizedBadRequest(t *testing.T) {
	for _, handler := range []gin.HandlerFunc{createGroup, updateGroup} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/group/create", strings.NewReader(`{"id":1,"name":"invalid","thinking_mode":"unsupported"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		handler(c)
		var response struct {
			MessageKey string `json:"message_key"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusBadRequest || response.MessageKey != "errors.invalidThinkingMode" {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}
