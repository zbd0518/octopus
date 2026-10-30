package backup

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internaldb "github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

func TestPluginBackupJSONRoundTripPreservesNamespaces(t *testing.T) {
	if err := internaldb.InitDB("sqlite", filepath.Join(t.TempDir(), "source.db"), false); err != nil {
		t.Fatalf("init source: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
	source := internaldb.GetDB()
	stamp := time.Now().UTC().Truncate(time.Second)
	for _, id := range []string{"plugin-alpha", "plugin-beta"} {
		if err := source.Create(&model.Plugin{
			ID: id, Name: id, Version: "1.0.0", ManifestJSON: `{}`, SHA256: "digest",
			Status: model.PluginStatusEnabled, Enabled: true, Calls: 3,
		}).Error; err != nil {
			t.Fatalf("seed plugin: %v", err)
		}
		if err := source.Create(&model.PluginKV{PluginID: id, Key: "shared", Value: id + "-value", UpdatedAt: stamp}).Error; err != nil {
			t.Fatalf("seed KV: %v", err)
		}
		for _, key := range []string{"shared", "empty"} {
			value := ""
			if key == "shared" {
				value = id + "-secret"
			}
			encrypted, err := crypto.Encrypt(value)
			if err != nil {
				t.Fatalf("encrypt source secret: %v", err)
			}
			if err := source.Create(&model.PluginSecret{PluginID: id, Key: key, Value: encrypted, UpdatedAt: stamp}).Error; err != nil {
				t.Fatalf("seed secret: %v", err)
			}
		}
	}
	dump, err := ExportAll(context.Background(), false, false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	resetCryptoKeyForTest(t, "octopus-plugin-backup-target-key")
	for _, mode := range []string{model.ImportModeFull, model.ImportModeIncremental} {
		t.Run(mode, func(t *testing.T) {
			target, err := internaldb.OpenStandalone("sqlite", filepath.Join(t.TempDir(), "target.db"), false)
			if err != nil {
				t.Fatalf("open target: %v", err)
			}
			t.Cleanup(func() {
				if conn, err := target.DB(); err == nil {
					_ = conn.Close()
				}
			})
			if err := internaldb.Migrate(target); err != nil {
				t.Fatalf("migrate target: %v", err)
			}
			var restored model.DBDump
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatalf("unmarshal backup: %v", err)
			}
			if _, err := ImportWithModeToDB(context.Background(), target, &restored, mode); err != nil {
				t.Fatalf("import: %v", err)
			}
			var entries []model.PluginKV
			if err := target.Find(&entries).Error; err != nil {
				t.Fatalf("query KV: %v", err)
			}
			if len(entries) != 2 {
				t.Fatalf("restored KV rows = %d, want 2 distinct namespaces", len(entries))
			}
			for _, entry := range entries {
				if entry.PluginID == "" || entry.Value != entry.PluginID+"-value" || !entry.UpdatedAt.Equal(stamp) {
					t.Fatalf("KV namespace, value or timestamp lost: %+v", entry)
				}
			}
			var secrets []model.PluginSecret
			if err := target.Find(&secrets).Error; err != nil {
				t.Fatalf("query secrets: %v", err)
			}
			if len(secrets) != 4 {
				t.Fatalf("restored secrets = %d, want 4 distinct namespace/key pairs", len(secrets))
			}
			for _, secret := range secrets {
				if secret.PluginID == "" || !secret.UpdatedAt.Equal(stamp) {
					t.Fatal("secret namespace or timestamp lost")
				}
				want := ""
				if secret.Key == "shared" {
					want = secret.PluginID + "-secret"
					if !crypto.IsEncrypted(secret.Value) {
						t.Fatal("import stored a plaintext secret")
					}
				}
				plain, err := crypto.Decrypt(secret.Value)
				if err != nil || plain != want {
					t.Fatalf("secret for %s/%s not restored under target encryption key: %v", secret.PluginID, secret.Key, err)
				}
			}
			var plugins []model.Plugin
			if err := target.Find(&plugins).Error; err != nil || len(plugins) != 2 {
				t.Fatalf("query restored plugins: %v (rows=%d)", err, len(plugins))
			}
			for _, record := range plugins {
				if record.Enabled || record.Status != model.PluginStatusNeedsReinstall || record.Calls != 0 {
					t.Fatalf("restored plugin is not safely disabled: %+v", record)
				}
			}
		})
	}
}

func TestPluginBackupRejectsMissingNamespace(t *testing.T) {
	if err := internaldb.InitDB("sqlite", filepath.Join(t.TempDir(), "invalid.db"), false); err != nil {
		t.Fatalf("init DB: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
	for _, table := range []string{"plugin_kv", "plugin_secrets"} {
		t.Run(table, func(t *testing.T) {
			var dump model.DBDump
			if err := json.Unmarshal([]byte(`{"`+table+`":[{"key":"shared","value":"lost-namespace"}]}`), &dump); err != nil {
				t.Fatal(err)
			}
			_, err := ImportWithMode(context.Background(), &dump, model.ImportModeIncremental)
			if err == nil || !strings.Contains(err.Error(), table+": missing plugin_id") {
				t.Fatalf("namespace-less backup was not rejected: %v", err)
			}
		})
	}
}

func TestPluginSecretManagementJSONRemainsRedacted(t *testing.T) {
	data, err := json.Marshal(model.PluginSecret{PluginID: "plugin-alpha", Key: "shared", Value: "private"})
	if err != nil {
		t.Fatalf("marshal management secret: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || string(fields["key"]) != `"shared"` {
		t.Fatalf("management secret exposed fields other than key: %s", data)
	}
}
