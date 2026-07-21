package relaylog

import (
	"testing"
	"time"
)

func TestPurgeExpiredStreamTokens(t *testing.T) {
	// 清空初始状态
	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens = make(map[string]time.Time)
	relayLogStreamTokensLock.Unlock()

	// 创建 3 个 token：2 个过期 + 1 个有效
	now := time.Now()
	expired1 := "expired_token_1"
	expired2 := "expired_token_2"
	valid := "valid_token"

	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens[expired1] = now.Add(-10 * time.Minute)
	relayLogStreamTokens[expired2] = now.Add(-6 * time.Minute)
	relayLogStreamTokens[valid] = now.Add(-1 * time.Minute)
	relayLogStreamTokensLock.Unlock()

	// 执行清理
	deleted := PurgeExpiredStreamTokens()
	if deleted != 2 {
		t.Fatalf("PurgeExpiredStreamTokens deleted = %d, want 2", deleted)
	}

	// 验证：过期的已删除，有效的保留
	relayLogStreamTokensLock.Lock()
	defer relayLogStreamTokensLock.Unlock()

	if _, ok := relayLogStreamTokens[expired1]; ok {
		t.Errorf("expired1 still exists after purge")
	}
	if _, ok := relayLogStreamTokens[expired2]; ok {
		t.Errorf("expired2 still exists after purge")
	}
	if _, ok := relayLogStreamTokens[valid]; !ok {
		t.Errorf("valid token was deleted")
	}
}

func TestPurgeExpiredStreamTokens_EmptyMap(t *testing.T) {
	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens = make(map[string]time.Time)
	relayLogStreamTokensLock.Unlock()

	deleted := PurgeExpiredStreamTokens()
	if deleted != 0 {
		t.Fatalf("PurgeExpiredStreamTokens on empty map deleted = %d, want 0", deleted)
	}
}

func TestPurgeExpiredStreamTokens_AllValid(t *testing.T) {
	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens = make(map[string]time.Time)
	now := time.Now()
	relayLogStreamTokens["valid1"] = now.Add(-1 * time.Minute)
	relayLogStreamTokens["valid2"] = now.Add(-2 * time.Minute)
	relayLogStreamTokensLock.Unlock()

	deleted := PurgeExpiredStreamTokens()
	if deleted != 0 {
		t.Fatalf("PurgeExpiredStreamTokens with all valid deleted = %d, want 0", deleted)
	}

	relayLogStreamTokensLock.Lock()
	defer relayLogStreamTokensLock.Unlock()
	if len(relayLogStreamTokens) != 2 {
		t.Fatalf("map size = %d, want 2", len(relayLogStreamTokens))
	}
}
