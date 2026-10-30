package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/conf"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/plugin"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/server/router"
	"gorm.io/gorm"
)

func init() {
	router.NewGroupRouter("/api/v1/plugins").Use(middleware.Auth()).Use(middleware.RequirePermission(auth.PermPluginsRead)).
		AddRoute(router.NewRoute("", http.MethodGet).Handle(listPlugins)).
		AddRoute(router.NewRoute("/:id/ui-bundle", http.MethodGet).Handle(pluginUIBundle))

	router.NewGroupRouter("/api/v1/plugins").Use(middleware.Auth()).Use(requirePluginAdmin).
		AddRoute(router.NewRoute("/install", http.MethodPost).Handle(installPlugin)).
		AddRoute(router.NewRoute("/:id/enable", http.MethodPost).Handle(enablePlugin)).
		AddRoute(router.NewRoute("/:id/disable", http.MethodPost).Handle(disablePlugin)).
		AddRoute(router.NewRoute("/:id/config", http.MethodPut).Handle(configurePlugin)).
		AddRoute(router.NewRoute("/:id", http.MethodDelete).Handle(uninstallPlugin))

	// One static dispatcher route avoids Gin's immutable route tree while retaining
	// authentication, authorization and audit on every plugin RPC request.
	router.NewGroupRouter("/api/v1/plugins/:id/rpc").Use(middleware.Auth()).
		AddRoute(router.NewRoute("/*path", http.MethodPost).Handle(pluginRPC)).
		AddRoute(router.NewRoute("/*path", http.MethodGet).Handle(pluginRPC))
}

func requirePluginAdmin(c *gin.Context) {
	if c.GetString("user_role") != model.UserRoleAdmin {
		resp.Error(c, http.StatusForbidden, "permission denied")
		c.Abort()
		return
	}
	c.Next()
}

func pluginManager(c *gin.Context) *plugin.Manager {
	if plugin.DefaultManager == nil {
		resp.Error(c, http.StatusServiceUnavailable, "plugin system is unavailable")
		return nil
	}
	return plugin.DefaultManager
}

func listPlugins(c *gin.Context) {
	items, err := plugin.ListRecords(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	for i := range items {
		items[i].LastError = pluginRedact(items[i].LastError)
	}
	resp.Success(c, gin.H{"plugins": items, "can_write": auth.HasPermission(c.GetString("user_role"), auth.PermPluginsWrite)})
}

func installPlugin(c *gin.Context) {
	manager := pluginManager(c)
	if manager == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, plugin.MaxPackageBytes+(1<<20))
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "plugin package file is required")
		return
	}
	defer file.Close()
	record, err := manager.Install(c.Request.Context(), file)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, pluginRedact(err.Error()))
		return
	}
	resp.Success(c, record)
}

func setPluginEnabled(c *gin.Context, enabled bool) {
	manager := pluginManager(c)
	if manager == nil {
		return
	}
	record, err := manager.SetEnabled(c.Request.Context(), c.Param("id"), enabled)
	pluginOperationResponse(c, record, err)
}
func enablePlugin(c *gin.Context)  { setPluginEnabled(c, true) }
func disablePlugin(c *gin.Context) { setPluginEnabled(c, false) }

func configurePlugin(c *gin.Context) {
	var request model.PluginConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := plugin.SaveConfig(c.Request.Context(), c.Param("id"), request.Config); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			resp.Error(c, http.StatusNotFound, "plugin not found")
			return
		}
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	record, err := plugin.Record(c.Request.Context(), c.Param("id"))
	pluginOperationResponse(c, record, err)
}

func uninstallPlugin(c *gin.Context) {
	manager := pluginManager(c)
	if manager == nil {
		return
	}
	var request model.PluginUninstallRequest
	if c.Request.Body != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			resp.InvalidJSON(c)
			return
		}
	}
	if err := manager.Uninstall(c.Request.Context(), c.Param("id"), request.DeleteData); err != nil {
		pluginOperationResponse(c, model.Plugin{}, err)
		return
	}
	resp.Success(c, gin.H{"restart_required": true})
}

func pluginOperationResponse(c *gin.Context, record model.Plugin, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		resp.Error(c, http.StatusNotFound, "plugin not found")
		return
	}
	if err != nil {
		resp.Error(c, http.StatusBadRequest, pluginRedact(err.Error()))
		return
	}
	record.LastError = pluginRedact(record.LastError)
	resp.Success(c, record)
}

func pluginRPC(c *gin.Context) {
	manager := pluginManager(c)
	if manager == nil {
		return
	}
	record, err := plugin.Record(c.Request.Context(), c.Param("id"))
	if err != nil {
		pluginOperationResponse(c, record, err)
		return
	}
	manifest, err := plugin.ParseManifest([]byte(record.ManifestJSON))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid installed plugin manifest")
		return
	}
	write := c.Request.Method != http.MethodGet
	permission := "rpc:read"
	if write {
		permission = "rpc:write"
	}
	if !manifest.HasPermission(permission) {
		resp.Error(c, http.StatusForbidden, "plugin RPC permission denied")
		return
	}
	if write && c.GetString("user_role") == model.UserRoleViewer {
		resp.Error(c, http.StatusForbidden, "permission denied")
		return
	}
	payload, ok := pluginRPCPayload(c)
	if !ok {
		return
	}
	result, err := manager.RPC(c.Request.Context(), record.ID, payload)
	if err != nil {
		resp.Error(c, http.StatusBadGateway, pluginRedact(err.Error()))
		return
	}
	resp.Success(c, json.RawMessage(result))
}

func pluginRPCPayload(c *gin.Context) ([]byte, bool) {
	envelope := gin.H{"method": c.Request.Method, "path": c.Param("path")}
	if c.Request.Method != http.MethodGet {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, plugin.MaxPayloadBytes+1))
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "invalid plugin request body")
			return nil, false
		}
		if len(body) > plugin.MaxPayloadBytes {
			resp.Error(c, http.StatusRequestEntityTooLarge, "plugin payload exceeds 1 MiB")
			return nil, false
		}
		if len(body) == 0 {
			body = []byte("null")
		}
		if !json.Valid(body) {
			resp.InvalidJSON(c)
			return nil, false
		}
		envelope["body"] = json.RawMessage(body)
	}
	payload, _ := json.Marshal(envelope)
	return payload, true
}

type pluginUIAsset struct {
	Content []byte `json:"content"`
	MIME    string `json:"mime"`
}

type pluginUIBundleResponse struct {
	Entry  string                   `json:"entry"`
	Assets map[string]pluginUIAsset `json:"assets"`
}

// UI bytes are returned only as authenticated JSON, never as host-origin HTML.
func pluginUIBundle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	record, err := plugin.Record(c.Request.Context(), c.Param("id"))
	if err != nil || !record.Enabled {
		resp.Error(c, http.StatusNotFound, "plugin UI not found")
		return
	}
	manifest, err := plugin.ParseManifest([]byte(record.ManifestJSON))
	if err != nil || manifest.UI == nil {
		resp.Error(c, http.StatusNotFound, "plugin UI not found")
		return
	}
	if version := c.Query("version"); version != "" && version != record.Version {
		resp.Error(c, http.StatusNotFound, "plugin asset version not found")
		return
	}
	root, err := plugin.PluginDir(filepath.Join(conf.DataDir(), "plugins"), record.ID)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid plugin asset path")
		return
	}
	bundle, err := readPluginUIBundle(root, manifest.UI.Entry)
	if err != nil {
		resp.Error(c, http.StatusNotFound, "plugin UI not found")
		return
	}
	resp.Success(c, bundle)
}

func readPluginUIBundle(root, entry string) (pluginUIBundleResponse, error) {
	bundle := pluginUIBundleResponse{Entry: entry, Assets: make(map[string]pluginUIAsset)}
	// os.Root confines reads even if a local file is replaced during traversal.
	installed, err := os.OpenRoot(root)
	if err != nil {
		return bundle, err
	}
	defer installed.Close()
	info, err := installed.Lstat("ui")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return bundle, errors.New("invalid UI directory")
	}
	ui, err := installed.OpenRoot("ui")
	if err != nil {
		return bundle, err
	}
	defer ui.Close()
	total := 0
	err = fs.WalkDir(ui.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("UI symlinks are not allowed")
		}
		if d.IsDir() {
			return nil
		}
		if !pluginSafeAssetPath(name) || !d.Type().IsRegular() || len(bundle.Assets) >= plugin.MaxPackageFiles {
			return errors.New("invalid UI asset")
		}
		file, err := ui.Open(name)
		if err != nil {
			return err
		}
		content, readErr := io.ReadAll(io.LimitReader(file, plugin.MaxAssetBytes+1))
		file.Close()
		total += len(content)
		if readErr != nil || len(content) > plugin.MaxAssetBytes || total > plugin.MaxPackageBytes {
			return errors.New("UI bundle exceeds size limit")
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		bundle.Assets[name] = pluginUIAsset{Content: content, MIME: contentType}
		return nil
	})
	if err == nil {
		if _, exists := bundle.Assets[entry]; !exists {
			err = errors.New("UI entry is missing")
		}
	}
	return bundle, err
}

func pluginSafeAssetPath(value string) bool {
	return value != "" && value != "." && !strings.ContainsAny(value, "\\\x00:") && !strings.HasPrefix(value, "/") && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../")
}

func pluginRedact(message string) string {
	return strings.ReplaceAll(strings.ReplaceAll(message, conf.AppConfig.Security.EncryptionKey, "[redacted]"), conf.AppConfig.Auth.JWTSecret, "[redacted]")
}
