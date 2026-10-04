package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	"github.com/lingyuins/octopus/internal/op/backup"
	stg "github.com/lingyuins/octopus/internal/op/setting"
)

// setupSettingTestDB 为单测准备独立 SQLite 与设置缓存（op.InitCache 的第一阶段
// 会 RefreshCache，把 DefaultSettings 补齐进缓存）。
func setupSettingTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := filepath.Join(t.TempDir(), "setting-test.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
}

func postSetting(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/set", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	setSetting(c)
	return recorder
}

// TestSetSettingInvalidIntervalNotPersisted 锁住：周期设置非法值返回 400 且
// 不得覆盖库内/缓存中的现值（后续 task.Update 的目标状态不被脏值污染）。
func TestSetSettingInvalidIntervalNotPersisted(t *testing.T) {
	setupSettingTestDB(t)

	for _, tc := range []struct {
		name string
		key  model.SettingKey
		body string
	}{
		{name: "stats save zero", key: model.SettingKeyStatsSaveInterval, body: `{"key":"stats_save_interval","value":"0"}`},
		{name: "model info zero", key: model.SettingKeyModelInfoUpdateInterval, body: `{"key":"model_info_update_interval","value":"0"}`},
		{name: "LLM sync zero", key: model.SettingKeySyncLLMInterval, body: `{"key":"sync_llm_interval","value":"0"}`},
		{name: "site sync zero", key: model.SettingKeySiteSyncInterval, body: `{"key":"site_sync_interval","value":"0"}`},
		{name: "site checkin negative", key: model.SettingKeySiteCheckinInterval, body: `{"key":"site_checkin_interval","value":"-1"}`},
		{name: "key health not a number", key: model.SettingKeyKeyHealthCheckInterval, body: `{"key":"key_health_check_interval","value":"abc"}`},
		{name: "pool token refresh zero", key: model.SettingKeyPoolTokenRefreshInterval, body: `{"key":"pool_token_refresh_interval","value":"0"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := stg.GetString(tc.key)
			if err != nil {
				t.Fatalf("get current value: %v", err)
			}

			recorder := postSetting(t, tc.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"message_key":"errors.inputValidationFailed"`) {
				t.Fatal("validation failure must include a localized message key")
			}
			var persisted model.Setting
			if err := db.GetDB().Where("key = ?", tc.key).First(&persisted).Error; err != nil {
				t.Fatalf("read persisted setting: %v", err)
			}
			if persisted.Value != before {
				t.Fatalf("invalid value written to database: %q", persisted.Value)
			}

			after, err := stg.GetString(tc.key)
			if err != nil {
				t.Fatalf("get value after rejected set: %v", err)
			}
			if after != before {
				t.Fatalf("invalid value must not be persisted: before=%q after=%q", before, after)
			}
		})
	}
}

// TestSetSettingValidIntervalPersisted 锁住：合法周期值持久化成功。
func TestSetSettingValidIntervalPersisted(t *testing.T) {
	setupSettingTestDB(t)

	recorder := postSetting(t, `{"key":"key_health_check_interval","value":"45"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}

	v, err := stg.GetString(model.SettingKeyKeyHealthCheckInterval)
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if v != "45" {
		t.Fatalf("persisted value = %q, want \"45\"", v)
	}
}

// TestSetSettingWebDAVConfigGenericPath 覆盖通用保存路径的 webdav_config：
// interval_hours 非法返回 400 且不持久化；合法值持久化并可被备份配置读取。
func TestSetSettingWebDAVConfigGenericPath(t *testing.T) {
	setupSettingTestDB(t)

	before, err := stg.GetString(model.SettingKeyWebDAVConfig)
	if err != nil {
		t.Fatalf("get webdav config: %v", err)
	}

	recorder := postSetting(t, `{"key":"webdav_config","value":"{\"enabled\":true,\"interval_hours\":0}"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	after, err := stg.GetString(model.SettingKeyWebDAVConfig)
	if err != nil {
		t.Fatalf("get webdav config after rejected set: %v", err)
	}
	if after != before {
		t.Fatalf("invalid webdav config must not be persisted: before=%q after=%q", before, after)
	}

	recorder = postSetting(t, `{"key":"webdav_config","value":"{\"enabled\":true,\"base_url\":\"https://dav.example.com\",\"remote_path\":\"/bk/\",\"interval_hours\":12,\"max_backups\":5}"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}

	cfg, err := backup.GetWebDAVConfig()
	if err != nil {
		t.Fatalf("GetWebDAVConfig: %v", err)
	}
	if cfg.IntervalHours != 12 {
		t.Fatalf("IntervalHours = %d, want 12", cfg.IntervalHours)
	}
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
}

// TestSetWebDAVConfigDedicatedEndpointValidates 覆盖专用保存端点：
// interval 越界返回 400 且不持久化；合法值持久化成功。
func TestSetWebDAVConfigDedicatedEndpointValidates(t *testing.T) {
	setupSettingTestDB(t)

	post := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/backup/webdav/config", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		setWebDAVConfig(c)
		return recorder
	}

	recorder := post(`{"enabled":true,"base_url":"https://dav.example.com","interval_hours":0,"max_backups":10}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if cfg, err := backup.GetWebDAVConfig(); err != nil || cfg.IntervalHours != 6 {
		t.Fatalf("rejected save must leave default config intact: cfg=%+v err=%v", cfg, err)
	}

	recorder = post(`{"enabled":true,"base_url":"https://dav.example.com","interval_hours":72,"max_backups":3}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	cfg, err := backup.GetWebDAVConfig()
	if err != nil {
		t.Fatalf("GetWebDAVConfig: %v", err)
	}
	if cfg.IntervalHours != 72 || cfg.MaxBackups != 3 {
		t.Fatalf("persisted cfg = %+v, want interval=72 max_backups=3", cfg)
	}
}
