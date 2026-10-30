package store

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestQuotaAtomicAdmissionAndDebt(t *testing.T) {
	for _, redisBackend := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "redis"}[redisBackend], func(t *testing.T) {
			ResetForTest()
			if redisBackend {
				server := miniredis.RunT(t)
				client := redis.NewClient(&redis.Options{Addr: server.Addr()})
				restore := InjectForTest(client)
				t.Cleanup(func() { restore(); client.Close() })
			}
			ctx := context.Background()
			id := 921001
			if redisBackend {
				id++
			}
			buckets := []QuotaBucket{{Name: "global:rpm", Limit: 10}, {Name: "global:tpm", Limit: 10, Tokens: true}, {Name: "model:a:rpm", Limit: 1}}
			first, err := ApplyQuota(ctx, id, "a", buckets, nil)
			if err != nil || !first.Allowed {
				t.Fatal(first, err)
			}
			denied, err := ApplyQuota(ctx, id, "b", buckets, nil)
			if err != nil || denied.Allowed || denied.RetryAfter < 1 || denied.RetryAfter > 60 {
				t.Fatal(denied, err)
			}
			usage := int64(20)
			if _, err := ApplyQuota(ctx, id, "missing-reservation", buckets, &usage); err == nil {
				t.Fatal("missing reservation was silently considered settled")
			}

			if _, err = ApplyQuota(ctx, id, "a", buckets, &usage); err != nil {
				t.Fatal(err)
			}
			if _, err = ApplyQuota(ctx, id, "a", buckets, &usage); err != nil {
				t.Fatal(err)
			}
			other := []QuotaBucket{{Name: "global:rpm", Limit: 10}, {Name: "global:tpm", Limit: 10, Tokens: true}, {Name: "model:b:rpm", Limit: 10}}
			denied, err = ApplyQuota(ctx, id, "c", other, nil)
			if err != nil || denied.Allowed || denied.RetryAfter < 60 || denied.RetryAfter > 67 {
				t.Fatal("debt or idempotent settlement failed", denied, err)
			}
		})
	}
}

func TestQuotaRefundAndGlobalScope(t *testing.T) {
	ResetForTest()
	ctx := context.Background()
	buckets := []QuotaBucket{{Name: "global:rpm", Limit: 2}, {Name: "global:tpm", Limit: 1, Tokens: true}}
	if result, err := ApplyQuota(ctx, 921003, "a", buckets, nil); err != nil || !result.Allowed {
		t.Fatal(result, err)
	}
	zero := int64(0)
	if _, err := ApplyQuota(ctx, 921003, "a", buckets, &zero); err != nil {
		t.Fatal(err)
	}
	if result, err := ApplyQuota(ctx, 921003, "b", buckets, nil); err != nil || !result.Allowed {
		t.Fatal("token reservation not refunded", result, err)
	}
	ApplyQuota(ctx, 921003, "b", buckets, &zero)
	if result, err := ApplyQuota(ctx, 921003, "c", buckets, nil); err != nil || result.Allowed {
		t.Fatal("global rpm bypass", result, err)
	}
}
