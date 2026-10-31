package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestStaticMiddlewareNeverInterceptsNodeHandshake(t *testing.T) {
	gin.SetMode(gin.TestMode)
	files := fstest.MapFS{"index.html": {Data: []byte("frontend shell")}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("frontend shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, serve := range map[string]gin.HandlerFunc{
		"embedded": StaticEmbed("/", files),
		"local":    StaticLocal("/", dir),
	} {
		t.Run(name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(serve)
			engine.GET("/node/v1/connect", func(c *gin.Context) {
				c.Status(http.StatusUnauthorized)
			})
			request := httptest.NewRequest(http.MethodGet, "/node/v1/connect", nil)
			request.Header.Set("Upgrade", "websocket")
			request.Header.Set("Connection", "Upgrade")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("Location") != "" {
				t.Fatalf("handshake was served as frontend: status=%d location=%q body=%q", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
			}
		})
	}
}
