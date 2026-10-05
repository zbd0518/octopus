package pooltokenrefresh

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
)

// --- CAS primitive: pool.UpdateAccountCredentialsIfUnchanged ---

// TestUpdateAccountCredentialsIfUnchanged_AppliesWithoutConcurrentChange: no
// concurrent writer — the CAS update applies and persists its columns.
func TestUpdateAccountCredentialsIfUnchanged_AppliesWithoutConcurrentChange(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	applied, err := pool.UpdateAccountCredentialsIfUnchanged(poolID, accountID, testRefreshCredJSON, map[string]interface{}{
		"token_expires_at": int64(12345),
	})
	if err != nil {
		t.Fatalf("CAS update: %v", err)
	}
	if !applied {
		t.Fatalf("CAS update must apply when credentials are unchanged")
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.TokenExpiresAt != 12345 {
		t.Fatalf("CAS update must persist its columns, token_expires_at=%d", acct.TokenExpiresAt)
	}
}

// TestUpdateAccountCredentialsIfUnchanged_LosesToConcurrentWriter: the
// three-step interleave — the refresher reads the old credentials, a
// concurrent writer stores new ones, and the CAS write-back must report
// not-applied, keep the rotated credentials alive and leak none of its
// columns.
func TestUpdateAccountCredentialsIfUnchanged_LosesToConcurrentWriter(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	// Step 2: a concurrent writer rotates the credentials after our snapshot.
	rotated := `{"type":"oauth","access_token":"at-rotated","refresh_token":"rt-rotated"}`
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{"credentials": rotated}); err != nil {
		t.Fatalf("concurrent writer: %v", err)
	}

	// Step 3: the stale CAS write-back loses.
	applied, err := pool.UpdateAccountCredentialsIfUnchanged(poolID, accountID, testRefreshCredJSON, map[string]interface{}{
		"credentials":      `{"type":"oauth","access_token":"at-stale","refresh_token":"rt-stale"}`,
		"token_expires_at": int64(999),
	})
	if err != nil {
		t.Fatalf("CAS update: %v", err)
	}
	if applied {
		t.Fatalf("CAS update must lose when credentials changed concurrently")
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.Credentials != rotated {
		t.Fatalf("rotated credentials must survive a lost CAS write, got %q", acct.Credentials)
	}
	if acct.TokenExpiresAt != 0 {
		t.Fatalf("lost CAS write must not leak any column, token_expires_at=%d", acct.TokenExpiresAt)
	}
}

// TestUpdateAccountCredentialsIfUnchanged_MissingAccountIsNoOp: a lost race
// and a vanished row are both reported as not-applied (no error).
func TestUpdateAccountCredentialsIfUnchanged_MissingAccountIsNoOp(t *testing.T) {
	ensureRefreshTestDB(t)
	applied, err := pool.UpdateAccountCredentialsIfUnchanged(999999, 999999, "whatever", map[string]interface{}{
		"token_expires_at": int64(1),
	})
	if err != nil {
		t.Fatalf("CAS update on a missing account must not error: %v", err)
	}
	if applied {
		t.Fatalf("CAS update on a missing account must report not-applied")
	}
}

// --- Refresh write-back integration with the CAS path ---

// TestRefreshSuccessDiscardedOnConcurrentRotation: the credentials are rotated
// by a concurrent writer while the refresh is in flight — the stale refresh
// result must be discarded (CAS miss), the rotated credentials must win, and
// the impl must return nil (the newer token wins; no error surfaces).
func TestRefreshSuccessDiscardedOnConcurrentRotation(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	rotated := `{"type":"oauth","access_token":"at-rotated","refresh_token":"rt-rotated"}`
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		// Simulate another instance rotating the credentials after our
		// snapshot but before our write-back.
		if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{"credentials": rotated}); err != nil {
			t.Errorf("concurrent rotation: %v", err)
		}
		newCred := cred
		newCred.AccessToken = "at-stale"
		return newCred, time.Now().Add(time.Hour).Unix(), nil
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err != nil {
		t.Fatalf("refreshAccountImpl must swallow a lost CAS race: %v", err)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.Credentials != rotated {
		t.Fatalf("rotated credentials must win the race, got %q", acct.Credentials)
	}
	if acct.TokenExpiresAt != 0 {
		t.Fatalf("stale result must not be persisted, token_expires_at=%d", acct.TokenExpiresAt)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("in-flight block should still be cleared, until=%d reason=%q", acct.TempUnschedUntil, acct.TempUnschedReason)
	}
}

// --- invalid_grant race recovery (B2-#3) ---

// TestRefreshInvalidGrantRecoversWithRotatedCredential: the first platform
// attempt fails with invalid_grant while the stored credentials have been
// rotated concurrently — the impl must re-read, retry exactly once with the
// rotated credential and finish through the normal success path (backoff
// cleared, recovered credentials persisted, no error surfaces).
func TestRefreshInvalidGrantRecoversWithRotatedCredential(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	rotated := `{"type":"oauth","access_token":"at-rotated","refresh_token":"rt-rotated"}`
	var calls atomic.Int64
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		if n := calls.Add(1); n == 1 {
			// Another instance rotates the credentials right before our stale
			// attempt is rejected with invalid_grant.
			if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{"credentials": rotated}); err != nil {
				t.Errorf("concurrent rotation: %v", err)
			}
			return cred, 0, errors.New(`token refresh failed: HTTP 400: {"error":"invalid_grant"}`)
		}
		if cred.RefreshToken != "rt-rotated" {
			t.Errorf("retry must use the rotated refresh_token, got %q", cred.RefreshToken)
		}
		newCred := cred
		newCred.AccessToken = "at-fresh"
		return newCred, time.Now().Add(time.Hour).Unix(), nil
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err != nil {
		t.Fatalf("invalid_grant recovery must succeed: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("platform refresh must run exactly twice (initial + one retry), got %d", got)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.GetExtra().RefreshFailureCount != 0 || acct.GetExtra().NextRefreshAllowedAt != 0 {
		t.Fatalf("backoff must be cleared after recovery, extra=%s", acct.Extra)
	}
	if acct.ErrorMessage != "" {
		t.Fatalf("error_message must be cleared after recovery, got %q", acct.ErrorMessage)
	}
	if !strings.Contains(acct.Credentials, "at-fresh") {
		t.Fatalf("recovered credentials must be persisted, got %q", acct.Credentials)
	}
}

// TestRefreshInvalidGrantWithoutRotationFallsBackToBackoff: invalid_grant with
// unchanged stored credentials is a genuine rejection — exactly one platform
// attempt, then the normal error_message + backoff window.
func TestRefreshInvalidGrantWithoutRotationFallsBackToBackoff(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	var calls atomic.Int64
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		calls.Add(1)
		return cred, 0, errors.New(`token refresh failed: HTTP 400: {"error":"invalid_grant"}`)
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err == nil {
		t.Fatalf("expected the invalid_grant failure to propagate")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("no retry may run when the credentials did not change, got %d calls", got)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.GetExtra().RefreshFailureCount != 1 {
		t.Fatalf("failure must record backoff count=1, got %d", acct.GetExtra().RefreshFailureCount)
	}
	if acct.GetExtra().NextRefreshAllowedAt <= time.Now().Unix() {
		t.Fatalf("a backoff window must be recorded, next_refresh_allowed_at=%d", acct.GetExtra().NextRefreshAllowedAt)
	}
	if !strings.Contains(acct.ErrorMessage, "invalid_grant") {
		t.Fatalf("error_message must carry the invalid_grant failure, got %q", acct.ErrorMessage)
	}
}

// TestRefreshInvalidClientGoesStraightToBackoff: failures other than
// invalid_grant (invalid_client etc.) must not trigger the recovery re-read —
// one attempt, straight to the backoff path.
func TestRefreshInvalidClientGoesStraightToBackoff(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	var calls atomic.Int64
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		calls.Add(1)
		return cred, 0, errors.New(`token refresh failed: HTTP 401: {"error":"invalid_client"}`)
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err == nil {
		t.Fatalf("expected the invalid_client failure to propagate")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("invalid_client must not be retried, got %d calls", got)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.GetExtra().RefreshFailureCount != 1 {
		t.Fatalf("failure must record backoff count=1, got %d", acct.GetExtra().RefreshFailureCount)
	}
	if !strings.Contains(acct.ErrorMessage, "invalid_client") {
		t.Fatalf("error_message must carry the invalid_client failure, got %q", acct.ErrorMessage)
	}
}
