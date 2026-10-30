package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/conf"
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/router"
)

func TestNodeManagementRBACOneTimeTokenAndAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "handlers.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	oldSecret := conf.AppConfig.Auth.JWTSecret
	conf.AppConfig.Auth.JWTSecret = "node-test-jwt"
	t.Cleanup(func() { conf.AppConfig.Auth.JWTSecret = oldSecret })
	engine := gin.New()
	engine.Use(middleware.AuditManagementWrite())
	group := &router.GroupRouter{Path: "/api/v1/nodes"}
	registerNodeRoutes(group)
	g := engine.Group(group.Path, group.Middlewares...)
	for _, r := range group.Routes {
		g.Handle(r.Method, r.Path, append(append([]gin.HandlerFunc{}, r.Middlewares...), r.Handlers...)...)
	}
	engine.GET("/node/v1/connect", connectNode)
	tokens := map[string]string{}
	for _, role := range []string{model.UserRoleAdmin, model.UserRoleEditor, model.UserRoleViewer} {
		u := model.User{Username: role, Role: role}
		if err := db.GetDB().Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		token, _, err := auth.GenerateJWTToken(60, u.ID, role)
		if err != nil {
			t.Fatal(err)
		}
		tokens[role] = token
	}
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if method == http.MethodPost || method == http.MethodPut {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	created := request(http.MethodPost, "/api/v1/nodes", `{"name":"edge"}`, tokens[model.UserRoleAdmin])
	if created.Code != 200 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var envelope struct {
		Data struct {
			Node  model.Node `json:"node"`
			Token string     `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Node.ID == 0 || envelope.Data.Token == "" || created.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing one-time token response")
	}
	id := envelope.Data.Node.ID
	for _, role := range []string{model.UserRoleAdmin, model.UserRoleEditor, model.UserRoleViewer} {
		read := request(http.MethodGet, "/api/v1/nodes", "", tokens[role])
		if read.Code != 200 || strings.Contains(read.Body.String(), envelope.Data.Token) || strings.Contains(read.Body.String(), "token_hash") {
			t.Fatalf("unsafe list for %s: %s", role, read.Body.String())
		}
		var list struct {
			Data struct {
				CanWrite bool `json:"can_write"`
			} `json:"data"`
		}
		if err := json.Unmarshal(read.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if list.Data.CanWrite != (role == model.UserRoleAdmin) {
			t.Fatalf("can_write for %s", role)
		}
		if role == model.UserRoleAdmin {
			continue
		}
		for _, op := range []struct{ method, path, body string }{
			{http.MethodPost, "/api/v1/nodes", `{"name":"forbidden"}`},
			{http.MethodPut, fmt.Sprintf("/api/v1/nodes/%d", id), `{"enabled":false}`},
			{http.MethodPost, fmt.Sprintf("/api/v1/nodes/%d/rotate-token", id), ""},
			{http.MethodDelete, fmt.Sprintf("/api/v1/nodes/%d", id), ""},
		} {
			if got := request(op.method, op.path, op.body, tokens[role]); got.Code != 403 {
				t.Fatalf("%s %s %s = %d", role, op.method, op.path, got.Code)
			}
		}
	}
	path := fmt.Sprintf("/api/v1/nodes/%d", id)
	rotated := request(http.MethodPost, path+"/rotate-token", "", tokens[model.UserRoleAdmin])
	if rotated.Code != 200 || strings.Contains(rotated.Body.String(), envelope.Data.Token) {
		t.Fatal("rotation did not return a new token")
	}
	if got := request(http.MethodPut, path, `{"name":"renamed","enabled":false}`, tokens[model.UserRoleAdmin]); got.Code != 200 || strings.Contains(got.Body.String(), "token") {
		t.Fatalf("update: %s", got.Body.String())
	}
	if got := request(http.MethodDelete, path, "", tokens[model.UserRoleAdmin]); got.Code != 200 {
		t.Fatalf("delete: %s", got.Body.String())
	}
	var audits []model.AuditLog
	if err := db.GetDB().Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) < 4 {
		t.Fatalf("node writes not audited: %d", len(audits))
	}
	encoded, err := json.Marshal(audits)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), envelope.Data.Token) {
		t.Fatal("audit leaked token")
	}
	for _, token := range []string{tokens[model.UserRoleAdmin], "sk-octopus-relay", envelope.Data.Token} {
		got := request(http.MethodGet, "/node/v1/connect", "", token)
		if got.Code != http.StatusUnauthorized {
			t.Fatalf("non-node or revoked tunnel token accepted: %d", got.Code)
		}
	}
}
