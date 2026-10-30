package relay

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	rl "github.com/lingyuins/octopus/internal/op/ratelimitstore"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/utils/log"
)

func reserveRelayQuota(c *gin.Context, model string) (*rl.Reservation, bool) {
	global := rl.Limits{RPM: c.GetInt("rate_limit_rpm"), TPM: c.GetInt("rate_limit_tpm")}
	var quotas map[string]rl.Limits
	if raw := c.GetString("per_model_quota_json"); raw != "" {
		if err := jsonAPI.Unmarshal([]byte(raw), &quotas); err != nil {
			log.Warnf("invalid API key quota configuration: key_id=%d", c.GetInt("api_key_id"))
			resp.Error(c, http.StatusServiceUnavailable, "quota configuration unavailable")
			return nil, false
		}
	}
	reservation, result, err := rl.Reserve(c.Request.Context(), c.GetInt("api_key_id"), model, global, quotas[model])
	if err != nil {
		resp.Error(c, http.StatusServiceUnavailable, "rate limit backend unavailable")
		return nil, false
	}
	if !result.Allowed {
		c.Header("Retry-After", strconv.Itoa(result.RetryAfter))
		c.Header("X-RateLimit-Remaining", "0")
		resp.Error(c, http.StatusTooManyRequests, "rate limit exceeded")
		return nil, false
	}
	if global.RPM > 0 || quotas[model].RPM > 0 {
		c.Header("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	}
	return reservation, true
}

func settleRelayQuota(reservation *rl.Reservation, usage int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := reservation.Settle(ctx, usage); err != nil {
		log.Warnf("relay quota settlement pending: backend unavailable")
	}
}
