package relaylog

import (
	"context"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
)

func TestRelayLogAddFullDisabledQueue(t *testing.T) {
	settings := setting.GetCache()
	oldSettings := settings.GetAll()
	t.Cleanup(func() {
		settings.Clear()
		for key, value := range oldSettings {
			settings.Set(key, value)
		}
	})
	settings.Set(model.SettingKeyRelayLogKeepEnabled, "true")
	settings.Set(model.SettingKeyRelayLogQueueDropPolicy, "disabled")
	defer SetCacheForTest(make([]model.RelayLog, relayLogMaxSize))()
	for len(flushCh) > 0 {
		<-flushCh
	}
	t.Cleanup(func() {
		for len(flushCh) > 0 {
			<-flushCh
		}
	})

	if err := RelayLogAdd(context.Background(), model.RelayLog{RequestModelName: "full-queue"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-flushCh:
	default:
		t.Fatal("full disabled queue did not request a flush")
	}
	if !relayLogCacheLock.TryLock() {
		t.Fatal("cache lock was not released")
	}
	defer relayLogCacheLock.Unlock()
	if len(relayLogCache) != relayLogMaxSize {
		t.Fatalf("cache size = %d, want %d", len(relayLogCache), relayLogMaxSize)
	}
}
