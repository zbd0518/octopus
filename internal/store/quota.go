package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/lingyuins/octopus/internal/conf"
	"github.com/redis/go-redis/v9"
)

type QuotaBucket struct {
	Name   string `json:"name"`
	Limit  int    `json:"limit"`
	Tokens bool   `json:"tokens"`
}

type QuotaResult struct {
	Allowed    bool
	Remaining  int
	RetryAfter int
}

type quotaState struct {
	Balance float64 `json:"balance"`
	Limit   int     `json:"limit"`
	Updated float64 `json:"updated"`
}

type quotaReceipt struct {
	Buckets   []QuotaBucket
	Settled   bool
	Created   time.Time
	SettledAt time.Time
}

type memoryQuota struct {
	buckets  map[string]quotaState
	receipts map[string]quotaReceipt
	seen     time.Time
}

var memoryQuotas = struct {
	sync.Mutex
	keys map[int]*memoryQuota
}{keys: make(map[int]*memoryQuota)}

const quotaReceiptRetention = 10 * time.Minute
const maxQuotaReceipts = 8192
const maxMemoryQuotaKeys = 16384

var ErrQuotaReceiptMissing = errors.New("quota reservation missing")

// ApplyQuota atomically reserves all buckets, or settles a reservation once.
// actual is nil for admission; settlement never rejects already consumed usage.
func ApplyQuota(ctx context.Context, keyID int, reservation string, buckets []QuotaBucket, actual *int64) (QuotaResult, error) {
	if actual != nil && *actual < 0 {
		return QuotaResult{}, fmt.Errorf("invalid quota usage")
	}
	for _, bucket := range buckets {
		if bucket.Limit <= 0 {
			return QuotaResult{}, fmt.Errorf("invalid quota bucket")
		}
	}
	mu.RLock()
	redisEnabled, backend := enabled, client
	mu.RUnlock()
	if !redisEnabled && conf.AppConfig.Cache.Type == "redis" {
		return QuotaResult{}, ErrBackendDisabled
	}
	if redisEnabled {
		if backend == nil {
			return QuotaResult{}, ErrBackendDisabled
		}
		encoded, err := json.Marshal(buckets)
		if err != nil {
			return QuotaResult{}, err
		}
		mode, usage := "reserve", int64(0)
		if actual != nil {
			mode, usage = "settle", *actual
		}
		result, err := quotaScript.Run(ctx, backend, []string{fmt.Sprintf("octopus:quota:v2:{%d}", keyID)}, mode, reservation, string(encoded), float64(time.Now().UnixNano())/1e9, usage).Int64Slice()
		if err != nil {
			if strings.Contains(err.Error(), ErrQuotaReceiptMissing.Error()) {
				return QuotaResult{}, ErrQuotaReceiptMissing
			}
			return QuotaResult{}, err
		}
		if len(result) != 3 {
			return QuotaResult{}, fmt.Errorf("invalid quota backend result")
		}
		return QuotaResult{Allowed: result[0] == 1, Remaining: int(result[1]), RetryAfter: int(result[2])}, nil
	}
	return applyMemoryQuota(keyID, reservation, buckets, actual)
}

func refillQuota(state quotaState, limit int, now float64) quotaState {
	if state.Limit == 0 {
		return quotaState{Balance: float64(limit), Limit: limit, Updated: now}
	}
	state.Balance = math.Min(float64(state.Limit), state.Balance+math.Max(0, now-state.Updated)*float64(state.Limit)/60)
	if state.Limit != limit {
		state.Balance += float64(limit - state.Limit)
		state.Limit = limit
	}
	state.Updated = now
	return state
}

func applyMemoryQuota(keyID int, reservation string, buckets []QuotaBucket, actual *int64) (QuotaResult, error) {
	memoryQuotas.Lock()
	defer memoryQuotas.Unlock()
	now := time.Now()
	for id, candidate := range memoryQuotas.keys {
		if memoryQuotaIdle(candidate, now) {
			delete(memoryQuotas.keys, id)
		}
	}
	state := memoryQuotas.keys[keyID]
	if state == nil {
		if actual != nil {
			return QuotaResult{}, ErrQuotaReceiptMissing
		}
		if len(memoryQuotas.keys) >= maxMemoryQuotaKeys {
			for id, candidate := range memoryQuotas.keys {
				if memoryQuotaIdle(candidate, now) {
					delete(memoryQuotas.keys, id)
				}
			}
			if len(memoryQuotas.keys) >= maxMemoryQuotaKeys {
				return QuotaResult{RetryAfter: 60}, nil
			}
		}
		state = &memoryQuota{buckets: make(map[string]quotaState), receipts: make(map[string]quotaReceipt)}
		memoryQuotas.keys[keyID] = state
	}
	state.seen = now
	seconds := float64(now.UnixNano()) / 1e9
	if receipt, exists := state.receipts[reservation]; exists {
		if actual == nil || receipt.Settled {
			return QuotaResult{Allowed: true}, nil
		}
		for _, bucket := range receipt.Buckets {
			if bucket.Tokens {
				current := state.buckets[bucket.Name]
				if current.Limit == 0 {
					return QuotaResult{}, ErrQuotaReceiptMissing
				}
			}
		}
		for _, bucket := range receipt.Buckets {
			if bucket.Tokens {
				current := state.buckets[bucket.Name]
				current = refillQuota(current, current.Limit, seconds)
				current.Balance = math.Min(float64(current.Limit), current.Balance-float64(*actual-1))
				state.buckets[bucket.Name] = current
			}
		}
		receipt.Settled = true
		receipt.SettledAt = now
		state.receipts[reservation] = receipt
		return QuotaResult{Allowed: true}, nil
	}
	if actual != nil {
		return QuotaResult{}, ErrQuotaReceiptMissing
	}
	for id, receipt := range state.receipts {
		if receipt.Settled && now.Sub(receipt.SettledAt) > quotaReceiptRetention {
			delete(state.receipts, id)
		}
	}
	if len(state.receipts) >= maxQuotaReceipts || len(state.buckets)+len(buckets) > maxQuotaReceipts {
		return QuotaResult{RetryAfter: 60}, nil
	}
	wait, remaining := 0, math.MaxInt
	for _, bucket := range buckets {
		current := refillQuota(state.buckets[bucket.Name], bucket.Limit, seconds)
		state.buckets[bucket.Name] = current
		if current.Balance < 1 {
			wait = max(wait, int(math.Ceil((1-current.Balance)*60/float64(bucket.Limit))))
		}
		if !bucket.Tokens {
			remaining = min(remaining, max(0, int(current.Balance)-1))
		}
	}
	if wait > 0 {
		return QuotaResult{RetryAfter: max(1, wait)}, nil
	}
	for _, bucket := range buckets {
		current := state.buckets[bucket.Name]
		current.Balance--
		state.buckets[bucket.Name] = current
	}
	state.receipts[reservation] = quotaReceipt{Buckets: append([]QuotaBucket(nil), buckets...), Created: now}
	if remaining == math.MaxInt {
		remaining = 0
	}
	return QuotaResult{Allowed: true, Remaining: remaining}, nil
}

func memoryQuotaIdle(state *memoryQuota, now time.Time) bool {
	if now.Sub(state.seen) < 24*time.Hour {
		return false
	}
	for _, receipt := range state.receipts {
		if !receipt.Settled || now.Sub(receipt.SettledAt) < quotaReceiptRetention {
			return false
		}
	}
	for _, bucket := range state.buckets {
		if refillQuota(bucket, bucket.Limit, float64(now.UnixNano())/1e9).Balance < float64(bucket.Limit) {
			return false
		}
	}
	return true
}

var quotaScript = redis.NewScript(`
local mode,id = ARGV[1],ARGV[2]
local buckets = cjson.decode(ARGV[3])
local now,actual = tonumber(ARGV[4]),tonumber(ARGV[5])
local receiptKey = 'r:'..id
local receipt = redis.call('HGET',KEYS[1],receiptKey)
local function refill(name,limit)
 local raw = redis.call('HGET',KEYS[1],'b:'..name)
 local s
 if raw then
  s=cjson.decode(raw)
  s.balance=math.min(s.limit,s.balance+math.max(0,now-s.updated)*s.limit/60)
  s.balance=s.balance+limit-s.limit
 else s={balance=limit} end
 s.limit=limit; s.updated=now
 return s
end
if receipt then
 local r=cjson.decode(receipt)
 if mode=='reserve' or r.settled then return {1,0,0} end
 for _,b in ipairs(r.buckets) do
  if b.tokens and redis.call('HEXISTS',KEYS[1],'b:'..b.name)==0 then
   return redis.error_reply('quota reservation missing')
  end
 end
 for _,b in ipairs(r.buckets) do
  if b.tokens then
   local raw=redis.call('HGET',KEYS[1],'b:'..b.name)
   if raw then
    local old=cjson.decode(raw)
    local s=refill(b.name,old.limit)
    s.balance=math.min(s.limit,s.balance-(actual-1))
    redis.call('HSET',KEYS[1],'b:'..b.name,cjson.encode(s))
   end
  end
 end
 r.settled=true; r.settled_at=now
 redis.call('HSET',KEYS[1],receiptKey,cjson.encode(r))
 local pending=redis.call('HINCRBY',KEYS[1],'pending',-1)
 if pending<=0 then
  local ttl=86400
  for _,field in ipairs(redis.call('HKEYS',KEYS[1])) do
   if string.sub(field,1,2)=='b:' then
    local b=cjson.decode(redis.call('HGET',KEYS[1],field))
    ttl=math.max(ttl,math.ceil((b.limit-b.balance)*60/b.limit)+600)
   end
  end
  redis.call('EXPIRE',KEYS[1],ttl)
 end
 return {1,0,0}
end
if mode=='settle' then return redis.error_reply('quota reservation missing') end
if redis.call('HLEN',KEYS[1])>=8192 then
 for _,field in ipairs(redis.call('HKEYS',KEYS[1])) do
  if string.sub(field,1,2)=='r:' then
   local r=cjson.decode(redis.call('HGET',KEYS[1],field))
   local settledAt=r.settled_at or r.created
   if r.settled and settledAt and now-settledAt>600 then redis.call('HDEL',KEYS[1],field) end
  end
 end
 if redis.call('HLEN',KEYS[1])+#buckets+1>=16384 then return {0,0,60} end
end
local states={}; local wait=0; local rem=2147483647
for i,b in ipairs(buckets) do
 local s=refill(b.name,b.limit); states[i]=s
 if s.balance<1 then wait=math.max(wait,math.ceil((1-s.balance)*60/b.limit)) end
 if not b.tokens then rem=math.min(rem,math.max(0,math.floor(s.balance)-1)) end
end
if wait>0 then return {0,0,wait} end
for i,b in ipairs(buckets) do
 local s=states[i]; s.balance=s.balance-1
 redis.call('HSET',KEYS[1],'b:'..b.name,cjson.encode(s))
end
redis.call('HSET',KEYS[1],receiptKey,cjson.encode({buckets=buckets,settled=false,created=now}))
redis.call('EXPIRE',KEYS[1],86400)
if rem==2147483647 then rem=0 end
return {1,rem,0}
`)
