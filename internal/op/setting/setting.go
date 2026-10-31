package setting

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/cache"
	"gorm.io/gorm"
)

var settingCache = cache.New[model.SettingKey, string](16)
var settingWriteMu sync.Mutex

// generation 在每次设置发生变更时自增。调用方可以缓存基于设置派生的
// 配置（如语义缓存运行时配置），只在代际变化时重新读取，避免在请求热
// 路径上反复读取多个设置并重建配置。
var generation atomic.Uint64

// Generation 返回当前设置代际。每当任意设置被写入（SetString/SetInt）或
// 缓存被整体刷新（RefreshCache）时该值都会改变。
func Generation() uint64 { return generation.Load() }

// GetCache returns the internal setting cache (for backward compatibility).
func GetCache() cache.Cache[model.SettingKey, string] { return settingCache }

func List(ctx context.Context) ([]model.Setting, error) {
	settings := make([]model.Setting, 0, settingCache.Len())
	for key, value := range settingCache.GetAll() {
		settings = append(settings, model.Setting{
			Key:   key,
			Value: value,
		})
	}
	return settings, nil
}

func GetString(key model.SettingKey) (string, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return "", fmt.Errorf("setting not found")
	}
	return setting, nil
}

func SetString(key model.SettingKey, value string) error {
	return SetStringBatch(context.Background(), map[model.SettingKey]string{key: value})
}

// SetStringBatch persists a group of settings in one transaction and publishes
// the new cache values only after the transaction commits.
func SetStringBatch(ctx context.Context, values map[model.SettingKey]string) error {
	return SetStringBatchWithUpdate(ctx, values, nil)
}

// SetStringBatchWithUpdate commits related DB changes with the settings before
// publishing the cache. update must not call another setting write operation.
func SetStringBatchWithUpdate(ctx context.Context, values map[model.SettingKey]string, update func(*gorm.DB) error) error {
	if len(values) == 0 {
		return nil
	}
	settingWriteMu.Lock()
	defer settingWriteMu.Unlock()

	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for key, value := range values {
			if _, ok := settingCache.Get(key); !ok {
				return fmt.Errorf("setting not found")
			}
			result := tx.Model(&model.Setting{Key: key}).Update("Value", value)
			if result.Error != nil {
				return fmt.Errorf("failed to set setting: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				// MySQL may report zero affected rows when the value is unchanged.
				// Confirm existence before treating the update as a missing key.
				var count int64
				if err := tx.Model(&model.Setting{}).Where("key = ?", key).Count(&count).Error; err != nil {
					return fmt.Errorf("failed to verify setting: %w", err)
				}
				if count == 0 {
					return fmt.Errorf("failed to set setting, key not found")
				}
			}
		}
		if update != nil {
			return update(tx)
		}
		return nil
	}); err != nil {
		return err
	}

	for key, value := range values {
		settingCache.Set(key, value)
	}
	generation.Add(1)
	return nil
}

func GetInt(key model.SettingKey) (int, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return 0, fmt.Errorf("setting not found")
	}
	return strconv.Atoi(setting)
}

func GetBool(key model.SettingKey) (bool, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return false, fmt.Errorf("setting not found")
	}
	return strconv.ParseBool(setting)
}

func SetInt(key model.SettingKey, value int) error {
	valueCache, ok := settingCache.Get(key)
	if !ok {
		return fmt.Errorf("setting not found")
	}
	valueCacheNum, err := strconv.Atoi(valueCache)
	if err != nil {
		return fmt.Errorf("failed to set setting: %w", err)
	}
	if valueCacheNum == value {
		return nil
	}
	return SetStringBatch(context.Background(), map[model.SettingKey]string{key: strconv.Itoa(value)})
}

func RefreshCache(ctx context.Context) error {
	settingWriteMu.Lock()
	defer settingWriteMu.Unlock()
	return refreshCache(ctx)
}

func refreshCache(ctx context.Context) error {
	conn := db.GetDB().WithContext(ctx)

	var settings []model.Setting
	if err := conn.Find(&settings).Error; err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	existingKeys := make(map[model.SettingKey]bool)
	for _, setting := range settings {
		existingKeys[setting.Key] = true
	}

	defaultSettings := model.DefaultSettings()
	missingSettings := make([]model.Setting, 0, len(defaultSettings))

	for _, defaultSetting := range defaultSettings {
		if !existingKeys[defaultSetting.Key] {
			missingSettings = append(missingSettings, defaultSetting)
		}
	}

	if len(missingSettings) > 0 {
		if err := conn.CreateInBatches(missingSettings, len(missingSettings)).Error; err != nil {
			return fmt.Errorf("failed to create missing settings: %w", err)
		}
		settings = append(settings, missingSettings...)
	}
	settingCache.Clear()
	for _, setting := range settings {
		settingCache.Set(setting.Key, setting.Value)
	}
	generation.Add(1)
	return nil
}
