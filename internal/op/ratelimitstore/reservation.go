package ratelimitstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lingyuins/octopus/internal/store"
)

type Limits struct {
	RPM int `json:"rpm"`
	TPM int `json:"tpm"`
}

type Reservation struct {
	mu      sync.Mutex
	keyID   int
	id      string
	buckets []store.QuotaBucket
	settled bool
}

func Reserve(ctx context.Context, keyID int, model string, global, specific Limits) (*Reservation, store.QuotaResult, error) {
	buckets := make([]store.QuotaBucket, 0, 4)
	add := func(prefix string, limit Limits) {
		if limit.RPM > 0 {
			buckets = append(buckets, store.QuotaBucket{Name: prefix + ":rpm", Limit: limit.RPM})
		}
		if limit.TPM > 0 {
			buckets = append(buckets, store.QuotaBucket{Name: prefix + ":tpm", Limit: limit.TPM, Tokens: true})
		}
	}
	add("global", global)
	hash := sha256.Sum256([]byte(strings.TrimSpace(model)))
	add("model:"+hex.EncodeToString(hash[:]), specific)
	reservation := &Reservation{keyID: keyID, id: uuid.NewString(), buckets: buckets}
	if len(buckets) == 0 {
		return reservation, store.QuotaResult{Allowed: true}, nil
	}
	if quotaSettlementOverflow.Load() || pendingQuotaSettlements.Load() >= 1024 {
		return nil, store.QuotaResult{}, fmt.Errorf("quota settlement backlog")
	}
	result, err := store.ApplyQuota(ctx, keyID, reservation.id, buckets, nil)
	if err != nil || !result.Allowed {
		return nil, result, err
	}
	return reservation, result, nil
}

func (r *Reservation) Settle(ctx context.Context, actual int64) error {
	if r == nil || len(r.buckets) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settled {
		return nil
	}
	actual = max(0, actual)
	_, err := store.ApplyQuota(ctx, r.keyID, r.id, r.buckets, &actual)
	if err == nil {
		r.settled = true
		return nil
	}
	startQuotaSettlementWorker()
	pendingQuotaSettlements.Add(1)
	select {
	case quotaSettlements <- quotaSettlement{reservation: r, actual: actual}:
		r.settled = true
	default:
		pendingQuotaSettlements.Add(-1)
		quotaSettlementOverflow.Store(true)
	}
	return err
}

type quotaSettlement struct {
	reservation *Reservation
	actual      int64
}

var pendingQuotaSettlements atomic.Int64
var quotaSettlementOverflow atomic.Bool
var quotaSettlements = make(chan quotaSettlement, 1024)
var quotaWorkerOnce sync.Once

func WaitForSettlements(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for pendingQuotaSettlements.Load() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

func startQuotaSettlementWorker() {
	quotaWorkerOnce.Do(func() {
		go func() {
			for job := range quotaSettlements {
				for {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, err := store.ApplyQuota(ctx, job.reservation.keyID, job.reservation.id, job.reservation.buckets, &job.actual)
					cancel()
					if err == nil {
						pendingQuotaSettlements.Add(-1)
						break
					}
					if errors.Is(err, store.ErrQuotaReceiptMissing) {
						// The reservation's backend record expired or was lost. Retrying
						// cannot restore it, so release the shutdown wait slot.
						pendingQuotaSettlements.Add(-1)
						break
					}
					time.Sleep(time.Second)
				}
			}
		}()
	})
}
