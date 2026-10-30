package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/crypto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ListRecords(ctx context.Context) ([]model.Plugin, error) {
	var records []model.Plugin
	return records, db.GetDB().WithContext(ctx).Order("id").Find(&records).Error
}

func Record(ctx context.Context, id string) (model.Plugin, error) {
	var record model.Plugin
	if !pluginIDPattern.MatchString(id) {
		return record, fmt.Errorf("invalid plugin id")
	}
	return record, db.GetDB().WithContext(ctx).First(&record, "id = ?", id).Error
}

func decodeManifest(record model.Plugin) (Manifest, error) {
	return ParseManifest([]byte(record.ManifestJSON))
}

func SaveConfig(ctx context.Context, id string, config map[string]any) error {
	if len(config) > 64 {
		return fmt.Errorf("too many plugin configuration fields")
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if len(encoded) > 64<<10 {
		return fmt.Errorf("plugin configuration is too large")
	}
	if strings.Contains(strings.ToLower(string(encoded)), "secret") {
		// Secrets have a dedicated encrypted API and must not be mixed into ordinary config.
		for key := range config {
			if strings.Contains(strings.ToLower(key), "secret") {
				return fmt.Errorf("secrets must use the secret API")
			}
		}
	}
	result := db.GetDB().WithContext(ctx).Model(&model.Plugin{}).Where("id = ?", id).Update("config_json", string(encoded))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func Config(ctx context.Context, id string) (map[string]any, error) {
	record, err := Record(ctx, id)
	if err != nil {
		return nil, err
	}
	config := map[string]any{}
	if record.ConfigJSON != "" {
		err = json.Unmarshal([]byte(record.ConfigJSON), &config)
	}
	return config, err
}

func validateDataKey(key string) error {
	if key == "" || len(key) > 256 || strings.ContainsRune(key, '\x00') {
		return fmt.Errorf("invalid plugin data key")
	}
	return nil
}

func KVGet(ctx context.Context, id, key string) (string, bool, error) {
	if err := validateDataKey(key); err != nil {
		return "", false, err
	}
	var entry model.PluginKV
	err := db.GetDB().WithContext(ctx).First(&entry, "plugin_id = ? AND key = ?", id, key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return entry.Value, err == nil, err
}

func KVSet(ctx context.Context, id, key, value string) error {
	if err := validateDataKey(key); err != nil {
		return err
	}
	if len(value) > 64<<10 {
		return fmt.Errorf("plugin KV value is too large")
	}
	return db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "plugin_id"}, {Name: "key"}}, UpdateAll: true}).Create(&model.PluginKV{PluginID: id, Key: key, Value: value}).Error
}

func KVDelete(ctx context.Context, id, key string) error {
	if err := validateDataKey(key); err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Delete(&model.PluginKV{}, "plugin_id = ? AND key = ?", id, key).Error
}

func KVList(ctx context.Context, id, prefix string) ([]string, error) {
	if len(prefix) > 256 {
		return nil, fmt.Errorf("plugin KV prefix is too long")
	}
	var keys []string
	return keys, db.GetDB().WithContext(ctx).Model(&model.PluginKV{}).Where("plugin_id = ? AND key LIKE ?", id, prefix+"%").Order("key").Limit(1000).Pluck("key", &keys).Error
}

func SecretGet(ctx context.Context, id, key string) (string, bool, error) {
	if err := validateDataKey(key); err != nil {
		return "", false, err
	}
	var entry model.PluginSecret
	err := db.GetDB().WithContext(ctx).First(&entry, "plugin_id = ? AND key = ?", id, key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value, err := crypto.Decrypt(entry.Value)
	return value, err == nil, err
}

func SecretSet(ctx context.Context, id, key, value string) error {
	if err := validateDataKey(key); err != nil {
		return err
	}
	if len(value) > 64<<10 {
		return fmt.Errorf("plugin secret is too large")
	}
	encrypted, err := crypto.Encrypt(value)
	if err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "plugin_id"}, {Name: "key"}}, UpdateAll: true}).Create(&model.PluginSecret{PluginID: id, Key: key, Value: encrypted}).Error
}

func SecretDelete(ctx context.Context, id, key string) error {
	if err := validateDataKey(key); err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Delete(&model.PluginSecret{}, "plugin_id = ? AND key = ?", id, key).Error
}
