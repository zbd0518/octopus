package poolscheduler

import (
	"sync/atomic"
	"testing"
	"time"
)

// resetAuthErrorsForTest 清空全局计数器（测试隔离）。
func resetAuthErrorsForTest(t *testing.T) {
	t.Helper()
	globalAuthErrors.Range(func(k, _ interface{}) bool {
		globalAuthErrors.Delete(k)
		return true
	})
	t.Cleanup(func() {
		globalAuthErrors.Range(func(k, _ interface{}) bool {
			globalAuthErrors.Delete(k)
			return true
		})
	})
}

func TestAuthErrorCounter_ThresholdExceeded(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 42

	var exceeded bool
	for i := 1; i <= 2; i++ {
		count, e := IncrementAuthError(poolID, accountID)
		if e {
			t.Fatalf("attempt %d unexpected exceeded", i)
		}
		if count != i {
			t.Fatalf("attempt %d count=%d want %d", i, count, i)
		}
	}
	_, exceeded = IncrementAuthError(poolID, accountID)
	if !exceeded {
		t.Fatalf("3rd attempt should exceed threshold")
	}
}

func TestAuthErrorCounter_WindowResets(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 7

	// 手动构造过期窗口：写入一个 windowStart 远超 180 分钟前。
	key := authErrorKey(poolID, accountID)
	globalAuthErrors.Store(key, &authErrorEntry{
		count:       2,
		windowStart: time.Now().Unix() - int64(authErrorWindow.Seconds()) - 10,
	})

	count, exceeded := IncrementAuthError(poolID, accountID)
	if exceeded {
		t.Fatalf("expired window should reset, not exceed")
	}
	if count != 1 {
		t.Fatalf("expired window count=%d want 1", count)
	}
}

func TestAuthErrorCounter_ResetClears(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 99

	for range 3 {
		IncrementAuthError(poolID, accountID)
	}
	ResetAuthError(poolID, accountID)

	count, exceeded := IncrementAuthError(poolID, accountID)
	if exceeded {
		t.Fatalf("post-reset first failure should not exceed")
	}
	if count != 1 {
		t.Fatalf("post-reset count=%d want 1", count)
	}
}

func TestAuthErrorCounter_RemoveAndPurge(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 300

	IncrementAuthError(poolID, accountID)
	RemoveAuthError(poolID, accountID)
	if _, ok := globalAuthErrors.Load(authErrorKey(poolID, accountID)); ok {
		t.Fatalf("RemoveAuthError should delete entry")
	}

	// 添一条 count=0 且窗口过期的记录，应被 Purge 清理。
	key := authErrorKey(poolID, accountID)
	globalAuthErrors.Store(key, &authErrorEntry{
		count:       0,
		windowStart: time.Now().Unix() - int64(authErrorWindow.Seconds()) - 10,
	})
	PurgeStaleAuthErrors()
	if _, ok := globalAuthErrors.Load(key); ok {
		t.Fatalf("PurgeStale should remove zero-count expired entry")
	}

	// 有计数的记录不应被删除。
	globalAuthErrors.Store(key, &authErrorEntry{
		count:       1,
		windowStart: time.Now().Unix(),
	})
	PurgeStaleAuthErrors()
	if _, ok := globalAuthErrors.Load(key); !ok {
		t.Fatalf("PurgeStale should keep active entry")
	}
}

func TestAuthErrorCounter_Concurrent(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 2, 500

	const N = 30
	done := make(chan struct{}, N)
	for range N {
		go func() {
			IncrementAuthError(poolID, accountID)
			done <- struct{}{}
		}()
	}
	for range N {
		<-done
	}
	val, _ := globalAuthErrors.Load(authErrorKey(poolID, accountID))
	entry := val.(*authErrorEntry)
	if c := atomic.LoadInt64(&entry.count); c != N {
		t.Fatalf("concurrent count=%d want %d", c, N)
	}
}
