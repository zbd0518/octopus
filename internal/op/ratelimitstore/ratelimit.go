package ratelimitstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lingyuins/octopus/internal/store"
	"github.com/lingyuins/octopus/internal/utils/ratelimit"
)

// rlKeyPrefix 是限流 bucket 在 KVStore/RateLimitStore 中的子系统前缀。
// 完整 Redis key 形如 octopus:rl:req:{apiKeyID}:{modelName} / octopus:rl:tok:{...}。
const (
	rlReqKeyPrefix = "rl:req:"
	rlTokKeyPrefix = "rl:tok:"
)

var (
	requestBuckets sync.Map // "apiKeyID:modelName" -> *ratelimit.TokenBucket
	tokenBuckets   sync.Map // "apiKeyID:modelName" -> *ratelimit.TokenBucket
)

func rateLimitKey(apiKeyID int, modelName string) string {
	return fmt.Sprintf("%d:%s", apiKeyID, modelName)
}

func rlReqKey(apiKeyID int, modelName string) string {
	return rlReqKeyPrefix + rateLimitKey(apiKeyID, modelName)
}

func rlTokKey(apiKeyID int, modelName string) string {
	return rlTokKeyPrefix + rateLimitKey(apiKeyID, modelName)
}

// CheckRateLimit checks if the request is within the rate limits.
// Returns: allowed, remaining requests, retry-after seconds.
func CheckRateLimit(apiKeyID int, modelName string, rpm int, tpm int, tokenCount int) (allowed bool, remaining int, retryAfter int) {
	// Redis 后端：token bucket 语义由 Lua 脚本原子执行（refill+consume）。
	// 降级（err）时回退内存路径，保证限流不失效（容错）。
	if store.Enabled() {
		rl := store.GetRateLimit()
		ctx := context.Background()
		// Redis 路径成功时直接返回；err 时回退内存路径（下方继续执行）。
		if rpm > 0 {
			ok, rem, resetAt, err := rl.CheckAndConsume(ctx, rlReqKey(apiKeyID, modelName), rpm, rpm, 1)
			if err == nil {
				if !ok {
					return false, 0, int(resetAt.Unix())
				}
				// RPM 通过；继续 TPM 检查（若有），否则直接返回。
				if tpm <= 0 {
					return true, rem, 0
				}
				remaining = rem
			}
		}
		if tpm > 0 {
			tc := tokenCount
			if tc <= 0 {
				tc = 1
			}
			// TPM 检查时 tokenCount 未知（传入 0），这里只做「预占 1」检查（与内存实现一致：
			// CheckRateLimit 用 tokenCount=0 调用，ConsumeTokens 才扣实际数）。
			ok, _, resetAt, err := rl.CheckAndConsume(ctx, rlTokKey(apiKeyID, modelName), tpm, tpm, tc)
			if err == nil {
				if !ok {
					return false, 0, int(resetAt.Unix())
				}
				// RPM（若有）已通过，TPM 也通过。
				return true, remaining, 0
			}
		}
		// 到这里说明 Redis 路径未返回（err 降级），继续内存路径。
	}

	key := rateLimitKey(apiKeyID, modelName)

	if rpm > 0 {
		reqBucket := getOrCreateBucket(&requestBuckets, key, rpm, rpm)
		if !reqBucket.Allow() {
			return false, 0, int(reqBucket.ResetAt().Unix())
		}
	}

	if tpm > 0 {
		tokenBucket := getOrCreateBucket(&tokenBuckets, key, tpm, tpm)
		if tokenCount <= 0 {
			tokenCount = 1
		}
		if !tokenBucket.AllowN(tokenCount) {
			return false, 0, int(tokenBucket.ResetAt().Unix())
		}
	}

	if rpm > 0 {
		reqBucket := getOrCreateBucket(&requestBuckets, key, rpm, rpm)
		remaining = reqBucket.TokensRemaining()
	}
	return true, remaining, 0
}

// ConsumeTokens deducts the actual token count from the rate limit bucket after a successful request.
func ConsumeTokens(apiKeyID int, modelName string, tpm int, tokenCount int) {
	if tpm <= 0 || tokenCount <= 0 {
		return
	}
	// Redis 后端：走 RateLimitStore.ConsumeTokens（Lua 原子扣减）。
	if store.Enabled() {
		_ = store.GetRateLimit().ConsumeTokens(context.Background(),
			rlTokKey(apiKeyID, modelName), tpm, tpm, tokenCount)
		return
	}
	key := rateLimitKey(apiKeyID, modelName)
	tokenBucket := getOrCreateBucket(&tokenBuckets, key, tpm, tpm)
	tokenBucket.Consume(tokenCount)
}

func getOrCreateBucket(m *sync.Map, key string, ratePerMinute int, burst int) *ratelimit.TokenBucket {
	if v, ok := m.Load(key); ok {
		if b, ok := v.(*ratelimit.TokenBucket); ok {
			return b
		}
	}
	bucket := ratelimit.NewTokenBucket(ratePerMinute, burst)
	actual, _ := m.LoadOrStore(key, bucket)
	if b, ok := actual.(*ratelimit.TokenBucket); ok {
		return b
	}
	return bucket
}

// PurgeStaleBuckets 清理长时间未活动的限流 bucket，防止全局 map 无界增长。
// bucket 的 key 含客户端请求携带的 modelName（基数不受控），与 balancer 全局 map
// 同维度（见 issue #46）。刷量/随机 model 名下只增不删会导致 requestBuckets /
// tokenBuckets 持续膨胀。maxAge 为最大空闲时长，超过则删除。由 relay log flush
// 定时任务周期性调用。返回删除的条目数。
func PurgeStaleBuckets(maxAge time.Duration) int {
	// Redis 模式下 bucket key 带 TTL（tokenBucketTTL），由 Redis 自动过期，无需主动清理。
	if store.Enabled() {
		return 0
	}
	if maxAge <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	purgeOne := func(m *sync.Map) {
		m.Range(func(k, v any) bool {
			b, ok := v.(*ratelimit.TokenBucket)
			if !ok {
				m.Delete(k)
				removed++
				return true
			}
			if b.LastUpdate().Before(cutoff) {
				m.Delete(k)
				removed++
			}
			return true
		})
	}
	purgeOne(&requestBuckets)
	purgeOne(&tokenBuckets)
	return removed
}

// RemoveAPIKeyBuckets 删除指定 API key 的所有限流 bucket（跨模型）。
// 在 API key 被删除时调用，避免其 bucket 残留驻留。
func RemoveAPIKeyBuckets(apiKeyID int) {
	if apiKeyID <= 0 {
		return
	}
	// Redis 后端：bucket key 含 TTL，删除时按 apiKeyID 前缀清理 req/tok 两类。
	if store.Enabled() {
		_ = store.GetRateLimit().RemoveByAPIKey(context.Background(), apiKeyID)
		return
	}
	prefix := fmt.Sprintf("%d:", apiKeyID)
	removeOne := func(m *sync.Map) {
		m.Range(func(k, _ any) bool {
			if s, ok := k.(string); ok && strings.HasPrefix(s, prefix) {
				m.Delete(k)
			}
			return true
		})
	}
	removeOne(&requestBuckets)
	removeOne(&tokenBuckets)
}
