package ratelimitstore

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lingyuins/octopus/internal/store"
	"github.com/redis/go-redis/v9"
)

func TestReservationGlobalModelAndWhitespace(t *testing.T) {
	store.ResetForTest()
	ctx := context.Background()
	global := Limits{RPM: 2, TPM: 10}
	first, result, err := Reserve(ctx, 939001, " model ", global, Limits{RPM: 1})
	if err != nil || !result.Allowed {
		t.Fatal(result, err)
	}
	if _, result, err := Reserve(ctx, 939001, "model", global, Limits{RPM: 1}); err != nil || result.Allowed {
		t.Fatal("whitespace split model quota", result, err)
	}
	if err := first.Settle(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if err := first.Settle(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if _, result, err := Reserve(ctx, 939001, "other", global, Limits{}); err != nil || result.Allowed {
		t.Fatal("global token debt bypassed", result, err)
	}
}

func TestReservationRedisUnavailableDoesNotFallback(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, DialTimeout: 50 * time.Millisecond})
	restore := store.InjectForTest(client)
	defer restore()
	defer client.Close()
	server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if reservation, _, err := Reserve(ctx, 939002, "model", Limits{RPM: 1}, Limits{}); err == nil || reservation != nil {
		t.Fatal("Redis failure silently fell back", err)
	}
}
