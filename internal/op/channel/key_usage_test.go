package channel

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func TestKeyUsagePreservesConfigurationAndDoesNotReinsert(t *testing.T) {
	ctx := context.Background()
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "keys.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	key := model.ChannelKey{ID: 919100, ChannelID: 919101, Enabled: true, ChannelKey: "original", TotalCost: 5}
	if err := db.GetDB().Create(&model.Channel{ID: key.ChannelID, Name: t.Name(), GroupID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	keyCache.Set(key.ID, key)
	chCache.Set(key.ChannelID, model.Channel{ID: key.ChannelID, Keys: []model.ChannelKey{key}})
	t.Cleanup(func() {
		keyCache.Del(key.ID)
		chCache.Del(key.ChannelID)
		runtimeUpdateLock.Lock()
		delete(pendingKeyUsage, key.ID)
		runtimeUpdateLock.Unlock()
		keyCacheNeedUpdateLock.Lock()
		delete(keyCacheNeedUpdate, key.ID)
		keyCacheNeedUpdateLock.Unlock()
	})
	if err := db.GetDB().Model(&key).Updates(map[string]any{"enabled": false, "channel_key": "rotated"}).Error; err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := RecordKeyUsage(key.ChannelID, key.ID, 0.5, 0, 100); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if err := KeySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	var stored model.ChannelKey
	if err := db.GetDB().First(&stored, key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Enabled || stored.ChannelKey != "rotated" || stored.TotalCost != 15 {
		t.Fatalf("configuration or cost corrupted: %+v", stored)
	}
	if err := db.GetDB().Delete(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if err := RecordKeyUsage(key.ChannelID, key.ID, 1, 0, 101); err != nil {
		t.Fatal(err)
	}
	if err := KeySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.GetDB().Model(&model.ChannelKey{}).Where("id = ?", key.ID).Count(&count)
	if count != 0 {
		t.Fatal("deleted key was reinserted")
	}
}

func TestKeyFeedbackRejectsPoolAndMissingKeys(t *testing.T) {
	channelID, keyID := 919102, 919103
	key := model.ChannelKey{ID: keyID, ChannelID: channelID, Enabled: true}
	chCache.Set(channelID, model.Channel{ID: channelID, PoolID: 1, Keys: []model.ChannelKey{key}})
	keyCache.Set(keyID, key)
	defer chCache.Del(channelID)
	defer keyCache.Del(keyID)
	if err := RecordKeyUsage(channelID, keyID, 1, 0, 1); err == nil {
		t.Fatal("pool credential accepted as ordinary key")
	}
	keyCache.Del(keyID)
	if err := KeyUpdate(key); err == nil {
		t.Fatal("missing key accepted")
	}
}
