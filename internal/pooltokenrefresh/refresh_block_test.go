package pooltokenrefresh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
)

// 共享测试 DB（Windows 上 TempDir 句柄跨测试保持打开，不能每测试重建）。
var (
	refreshTestDBOnce sync.Once
	refreshTestDBErr  error
)

func ensureRefreshTestDB(t *testing.T) {
	t.Helper()
	refreshTestDBOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pooltokenrefresh-test-*")
		if err != nil {
			refreshTestDBErr = err
			return
		}
		dsn := filepath.Join(dir, "refresh-test.db")
		refreshTestDBErr = db.InitDB("sqlite", dsn, false)
	})
	if refreshTestDBErr != nil {
		t.Fatalf("init shared refresh test db: %v", refreshTestDBErr)
	}
}

var refreshAccountSeq int64

// createRefreshTestAccount 创建独立池 + OAuth 账号（凭据为明文 JSON，
// crypto 未初始化时 EncryptCredentials 原样透传）。
func createRefreshTestAccount(t *testing.T, credJSON string) (poolID, accountID int) {
	t.Helper()
	ensureRefreshTestDB(t)
	seq := atomic.AddInt64(&refreshAccountSeq, 1)
	p := &model.AccountPool{
		Name:               fmt.Sprintf("refresh-test-%d", seq),
		Strategy:           "ewma",
		DefaultConcurrency: 1,
		CooldownBaseSec:    300,
		Enabled:            true,
	}
	if err := pool.CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	a := &model.PoolAccount{
		PoolID:      p.ID,
		Name:        fmt.Sprintf("acct-%d", seq),
		Platform:    model.PoolPlatformOpenAI,
		Type:        model.PoolTypeOAuth,
		Credentials: credJSON,
	}
	if err := pool.CreateAccount(a); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		poolscheduler.RemovePool(p.ID)
		_ = pool.DeletePool(p.ID)
	})
	return p.ID, a.ID
}

// overrideRefreshByPlatform 替换平台刷新实现并在测试结束后还原。
func overrideRefreshByPlatform(t *testing.T, fn func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error)) {
	t.Helper()
	prev := refreshByPlatformFunc
	refreshByPlatformFunc = fn
	t.Cleanup(func() { refreshByPlatformFunc = prev })
}

const testRefreshCredJSON = `{"type":"oauth","access_token":"at-old","refresh_token":"rt-old"}`

// TestRefreshInFlightBlocksScheduling：刷新在途期间账号被临时禁用
//（并发调度管线的 ListSchedulableAccounts 不可见），完成后 1s 内恢复可调度。
func TestRefreshInFlightBlocksScheduling(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	inFlight := make(chan struct{})
	release := make(chan struct{})
	observedBlocked := false
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		// 刷新在途：模拟并发调度扫描，账号必须被临时不可调度挡住。
		candidates, err := pool.ListSchedulableAccounts(poolID)
		if err != nil {
			t.Errorf("list schedulable during refresh: %v", err)
		}
		for i := range candidates {
			if candidates[i].ID == accountID {
				t.Errorf("account %d should be temp-unsched while refresh is in flight", accountID)
			}
		}
		observedBlocked = true
		close(inFlight)
		<-release
		newCred := cred
		newCred.AccessToken = "at-new"
		return newCred, time.Now().Add(time.Hour).Unix(), nil
	})

	errCh := make(chan error, 1)
	go func() { errCh <- refreshAccountImpl(context.Background(), poolID, accountID) }()
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatalf("refresh never reached the platform stub")
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("refreshAccountImpl: %v", err)
	}
	if !observedBlocked {
		t.Fatalf("stub did not observe the in-flight window")
	}

	// 完成后 1s 内恢复可调度。
	deadline := time.Now().Add(time.Second)
	for {
		candidates, err := pool.ListSchedulableAccounts(poolID)
		if err != nil {
			t.Fatalf("list schedulable after refresh: %v", err)
		}
		found := false
		for i := range candidates {
			if candidates[i].ID == accountID {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("account %d not schedulable within 1s after refresh", accountID)
		}
		time.Sleep(10 * time.Millisecond)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("block should be cleared after success, until=%d reason=%q", acct.TempUnschedUntil, acct.TempUnschedReason)
	}
}

// TestRefreshFailureClearsBlock：失败路径同样清除在途块（退避由
// next_refresh_allowed_at 负责，与块无关）。
func TestRefreshFailureClearsBlock(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		return cred, 0, errors.New("upstream refresh rejected")
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err == nil {
		t.Fatalf("expected refresh failure error")
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("block should be cleared after failure, until=%d reason=%q", acct.TempUnschedUntil, acct.TempUnschedReason)
	}
	if acct.GetExtra().RefreshFailureCount != 1 {
		t.Fatalf("failure should record backoff count=1, got %d", acct.GetExtra().RefreshFailureCount)
	}
}

// TestRefreshNoRefreshTokenLeavesNoBlock：无 refresh_token 的早返回
// 发生在设块之前，不留任何块，也不触发平台刷新。
func TestRefreshNoRefreshTokenLeavesNoBlock(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, `{"type":"oauth","access_token":"at-only"}`)

	called := false
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		called = true
		return cred, 0, nil
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err == nil {
		t.Fatalf("expected no-refresh_token error")
	}
	if called {
		t.Fatalf("platform refresh must not run without refresh_token")
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("no block should be left by the early return")
	}
}

// TestClearTempUnschedIfTrigger_OnlyClearsOwnBlock：条件清除不擦并发
// 401/403/手动块；自己的 trigger 块被清。
func TestClearTempUnschedIfTrigger_OnlyClearsOwnBlock(t *testing.T) {
	ensureRefreshTestDB(t)
	seq := atomic.AddInt64(&refreshAccountSeq, 1)
	p := &model.AccountPool{Name: fmt.Sprintf("clear-trigger-%d", seq), Strategy: "ewma", CooldownBaseSec: 300, Enabled: true}
	if err := pool.CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() { _ = pool.DeletePool(p.ID) })
	a := &model.PoolAccount{PoolID: p.ID, Name: "acct", Platform: model.PoolPlatformOpenAI, Type: model.PoolTypeOAuth}
	if err := pool.CreateAccount(a); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// 403 形状的块（与 handlePoolAuthError setTempUnschedWithReason 同形状）。
	blockedUntil := time.Now().Add(10 * time.Minute)
	reason403 := `{"status_code":403,"trigger":"http_403_counter","at":1759500000}`
	poolscheduler.SetTempUnsched(p.ID, a.ID, blockedUntil, reason403)

	cleared, err := poolscheduler.ClearTempUnschedIfTrigger(p.ID, a.ID, refreshUnschedTrigger)
	if err != nil {
		t.Fatalf("conditional clear: %v", err)
	}
	if cleared {
		t.Fatalf("must not clear a 403-owned block")
	}
	acct, err := pool.GetAccount(p.ID, a.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.TempUnschedReason != reason403 || !acct.IsTempUnsched() {
		t.Fatalf("403 block must survive, got reason=%q until=%d", acct.TempUnschedReason, acct.TempUnschedUntil)
	}

	// 自己的 trigger 块 → 清除成功。
	poolscheduler.SetTempUnsched(p.ID, a.ID, time.Now().Add(6*time.Minute), `{"trigger":"token_refresh_inflight","at":1}`)
	cleared, err = poolscheduler.ClearTempUnschedIfTrigger(p.ID, a.ID, refreshUnschedTrigger)
	if err != nil {
		t.Fatalf("conditional clear own block: %v", err)
	}
	if !cleared {
		t.Fatalf("own block should be cleared")
	}
	acct, err = pool.GetAccount(p.ID, a.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("own block should be gone, got reason=%q until=%d", acct.TempUnschedReason, acct.TempUnschedUntil)
	}

	// 空白账号：条件清除是无害 no-op。
	if cleared, err := poolscheduler.ClearTempUnschedIfTrigger(p.ID, a.ID, refreshUnschedTrigger); err != nil || cleared {
		t.Fatalf("clear on unblocked account should be a no-op, cleared=%v err=%v", cleared, err)
	}
}
