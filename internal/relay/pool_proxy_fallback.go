package relay

import (
	"strings"

	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/utils/log"
)

// pool_proxy_fallback.go — B4-#13: proxy-layer failure classification and the
// feedback hook that switches an account to its configured backup proxy.

// proxyFailureMarkers are transport-layer error markers (lower-cased
// substrings) that indicate the dial/CONNECT stage failed rather than the
// upstream answering with a status. http.Client surfaces proxy dial failures
// as "proxyconnect tcp: ..." and direct/upstream dial failures as
// "dial tcp ...". Deliberately NOT matched: generic "context deadline
// exceeded" / timeouts — a slow upstream is not proxy evidence, and flipping
// accounts on every upstream stall would churn configs. The wrapped error
// chain produced by forward()/attempt() keeps the underlying transport text,
// so substring matching works on result.Err.
var proxyFailureMarkers = []string{
	"proxyconnect",
	"dial tcp",
	"dial unix",
	"connect: connection refused",
	"no such host",
	"network is unreachable",
}

// isProxyLayerFailure reports whether the attempt error looks like a
// transport/dial-layer failure (direct dial or proxy CONNECT).
func isProxyLayerFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range proxyFailureMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// maybeEnterProxyFallback engages the account's configured backup proxy after
// a proxy-layer failure. Backup-proxy-only policy: without a configured
// backup this is a no-op (never an automatic direct connection); the
// op/pool guarded update prevents concurrent writers from overwriting the
// recorded origin.
func maybeEnterProxyFallback(poolID, accountID int) {
	entered, err := pool.EnterProxyFallback(poolID, accountID)
	if err != nil {
		log.Warnf("pool proxy fallback: switch account %d/%d failed: %v", poolID, accountID, err)
		return
	}
	if entered {
		log.Infof("pool proxy fallback: account %d/%d switched to its backup proxy (origin recorded)", poolID, accountID)
	}
}
