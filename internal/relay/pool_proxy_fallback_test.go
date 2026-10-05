package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
)

// TestIsProxyLayerFailure locks in the classification: dial/CONNECT-stage
// transport errors match; upstream HTTP failures, timeouts and client
// disconnects do not.
func TestIsProxyLayerFailure(t *testing.T) {
	matching := []string{
		`channel X adapter=openai attempt 1/3: failed to send request: Post "https://api.openai.com/v1/chat/completions": proxyconnect tcp: dial tcp 10.0.0.9:8080: connect: connection refused`,
		`channel X attempt 1/3: failed to send request: Post "https://api.example.com/v1/chat/completions": dial tcp 1.2.3.4:443: connect: connection refused`,
		`failed to send request: Post "https://api.example.com": dial tcp: lookup api.example.com: no such host`,
	}
	for _, msg := range matching {
		if !isProxyLayerFailure(errors.New(msg)) {
			t.Fatalf("expected proxy-layer classification for: %s", msg)
		}
	}
	notMatching := []string{
		`channel X attempt 1/3: upstream error: 429: {"error":"rate limited"}`,
		`context deadline exceeded`,
		`failed to send request: Post "https://api.example.com": context canceled`,
		`empty output, try another key`,
		`upstream url is not allowed: resolve url host: lookup invalid.example: no such host`,
		`upstream error: 502: {"error":"dial tcp: connect: connection refused"}`,
	}
	for _, msg := range notMatching {
		if isProxyLayerFailure(errors.New(msg)) {
			t.Fatalf("expected non-proxy classification for: %s", msg)
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := fmt.Errorf("failed to send request: %w", &net.OpError{Op: "dial", Net: "tcp", Err: cause})
		if isProxyLayerFailure(err) {
			t.Fatalf("canceled/deadline dial must not trigger fallback: %v", err)
		}
	}
	if isProxyLayerFailure(nil) {
		t.Fatalf("nil error must not classify as proxy failure")
	}
}

// TestMaybeEnterProxyFallbackFromRelayHook covers the composition the relay
// feedback segment performs: classification + guarded switch to the backup
// proxy, and a no-op for accounts without a backup (never auto-direct).
func TestMaybeEnterProxyFallbackFromRelayHook(t *testing.T) {
	setupUnschedRuleRelayDB(t)

	newAccount := func(t *testing.T, withBackup bool) (*model.PoolAccount, *int) {
		t.Helper()
		p := &model.AccountPool{Name: nextPoolTestName("relay-fallback", t), Enabled: true}
		if err := pool.CreatePool(p); err != nil {
			t.Fatalf("create pool: %v", err)
		}
		t.Cleanup(func() { _ = pool.DeletePool(p.ID) })
		primary := 77
		acct := &model.PoolAccount{
			PoolID:        p.ID,
			Name:          "relay-fallback-account",
			Platform:      model.PoolPlatformOpenAI,
			Type:          model.PoolTypeAPIKey,
			Credentials:   `{"type":"apikey","api_key":"sk-test"}`,
			ProxyConfigID: &primary,
		}
		var backup *int
		if withBackup {
			b := 88
			backup = &b
			acct.SetExtra(model.PoolAccountExtra{BackupProxyConfigID: backup})
		}
		if err := pool.CreateAccount(acct); err != nil {
			t.Fatalf("create account: %v", err)
		}
		return acct, backup
	}

	// With a backup configured: the dial failure flips the account to the
	// backup and records the origin.
	acct, backup := newAccount(t, true)
	maybeEnterProxyFallback(acct.PoolID, acct.ID)
	fresh, err := pool.GetAccount(acct.PoolID, acct.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if fresh.ProxyConfigID == nil || *fresh.ProxyConfigID != *backup {
		t.Fatalf("account must run on the backup proxy, got %v", fresh.ProxyConfigID)
	}
	if fresh.ProxyFallbackOriginID == nil || *fresh.ProxyFallbackOriginID != 77 {
		t.Fatalf("origin must be recorded, got %v", fresh.ProxyFallbackOriginID)
	}

	// Without a backup: no-op, proxy untouched, no marker.
	acct2, _ := newAccount(t, false)
	maybeEnterProxyFallback(acct2.PoolID, acct2.ID)
	fresh2, err := pool.GetAccount(acct2.PoolID, acct2.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if fresh2.ProxyConfigID == nil || *fresh2.ProxyConfigID != 77 {
		t.Fatalf("no-backup account must keep its proxy, got %v", fresh2.ProxyConfigID)
	}
	if fresh2.ProxyFallbackOriginID != nil {
		t.Fatalf("no-backup account must not get a fallback marker, got %v", fresh2.ProxyFallbackOriginID)
	}
}
