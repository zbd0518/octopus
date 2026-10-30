package handlers

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/conf"
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/plugin"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
)

func writePluginUIFile(t *testing.T, root, name string, content []byte) {
	t.Helper()
	target := filepath.Join(root, "ui", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPluginRPCPayloadPreservesGETPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, "/rpc/status/detail", strings.NewReader(`{"value":1}`))
		c.Params = gin.Params{{Key: "path", Value: "/status/detail"}}
		payload, ok := pluginRPCPayload(c)
		if !ok {
			t.Fatalf("%s payload rejected", method)
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if string(envelope["path"]) != `"/status/detail"` || string(envelope["method"]) != `"`+method+`"` {
			t.Fatalf("unexpected payload: %s", payload)
		}
		if method == http.MethodGet && envelope["body"] != nil {
			t.Fatalf("GET leaked a request body: %s", payload)
		}
		if method == http.MethodPost && string(envelope["body"]) != `{"value":1}` {
			t.Fatalf("POST lost body: %s", payload)
		}
	}
}

func TestPluginUIBundleAssetsAndLimits(t *testing.T) {
	root := t.TempDir()
	writePluginUIFile(t, root, "pages/index.html", []byte(`<script src="../app.js"></script>`))
	writePluginUIFile(t, root, "app.js", []byte(`console.log('local')`))
	writePluginUIFile(t, root, "style.css", []byte(`body{color:green}`))
	bundle, err := readPluginUIBundle(root, "pages/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Assets) != 3 || bundle.Entry != "pages/index.html" || !strings.HasPrefix(bundle.Assets["style.css"].MIME, "text/css") {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
	if _, err := readPluginUIBundle(root, "missing.html"); err == nil {
		t.Fatal("missing entry accepted")
	}
	writePluginUIFile(t, root, "too-big.bin", make([]byte, plugin.MaxAssetBytes+1))
	if _, err := readPluginUIBundle(root, "pages/index.html"); err == nil {
		t.Fatal("oversized asset accepted")
	}
	for _, name := range []string{"../secret", "/secret", "a\\b", ".", "a:stream", "a\x00b"} {
		if pluginSafeAssetPath(name) {
			t.Fatalf("unsafe path accepted: %q", name)
		}
	}
}

func TestPluginUIBundleRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	writePluginUIFile(t, root, "index.html", []byte("safe"))
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "ui", "linked")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := readPluginUIBundle(root, "index.html"); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestPluginUIBundleRequiresHeaderAuthAndEnabledVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("OCTOPUS_DATA_DIR", t.TempDir())
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "plugins.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	oldSecret := conf.AppConfig.Auth.JWTSecret
	conf.AppConfig.Auth.JWTSecret = rand.Text()
	t.Cleanup(func() { conf.AppConfig.Auth.JWTSecret = oldSecret })
	user := model.User{Username: "plugin-reader", Password: "not-used", Role: model.UserRoleAdmin}
	if err := db.GetDB().Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.GenerateJWTToken(60, user.ID, user.Role)
	if err != nil {
		t.Fatal(err)
	}
	manifest := `{"host_api":1,"id":"ui-test","name":"UI test","version":"1.0.0","ui":{"entry":"index.html","title":"Test"}}`
	record := model.Plugin{ID: "ui-test", Name: "UI test", Version: "1.0.0", ManifestJSON: manifest, Enabled: true}
	if err := db.GetDB().Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(conf.DataDir(), "plugins", record.ID)
	writePluginUIFile(t, root, "index.html", []byte("<h1>private UI</h1>"))
	engine := gin.New()
	engine.GET("/prefix/api/v1/plugins/:id/ui-bundle", middleware.Auth(), middleware.RequirePermission(auth.PermPluginsRead), pluginUIBundle)
	request := func(query, header string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/prefix/api/v1/plugins/ui-test/ui-bundle"+query, nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		return recorder
	}
	for _, query := range []string{"", "?token=" + token} {
		response := request(query, "")
		if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "private UI") {
			t.Fatal("UI was served without Authorization header")
		}
	}
	response := request("?version=1.0.0", "Bearer "+token)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated bundle: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unsafe headers: %v", response.Header())
	}
	var envelope struct {
		Data pluginUIBundleResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope.Data.Assets["index.html"].Content) != "<h1>private UI</h1>" {
		t.Fatalf("missing UI bytes: %s", response.Body.String())
	}
	if response := request("?version=old", "Bearer "+token); response.Code != http.StatusNotFound {
		t.Fatal("old version accepted")
	}
	if err := db.GetDB().Model(&record).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if response := request("", "Bearer "+token); response.Code != http.StatusNotFound {
		t.Fatal("disabled UI served")
	}
}
