package relay

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/helper"
	dm "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
)

func init() {
	helper.AcquirePoolProbeCredential = func(ctx context.Context, channel dm.Channel, model string) (dm.Channel, dm.ChannelKey, func(), error) {
		selected, key, lease, err := mediaPoolAccount(&channel, "", nil, model)
		if err != nil {
			return dm.Channel{}, dm.ChannelKey{}, nil, err
		}
		return *selected, key, lease.Release, nil
	}
}

func mediaPoolAccount(channel *dm.Channel, session string, excluded []int, model string) (*dm.Channel, dm.ChannelKey, *poolscheduler.AccountLease, error) {
	lease, err := poolscheduler.SelectAccountLease(channel.PoolID, session, excluded, 1, model)
	if err != nil {
		return nil, dm.ChannelKey{}, nil, err
	}
	account, credential, err := pool.CredentialsForRelay(lease.Account)
	if err != nil {
		lease.Release()
		return nil, dm.ChannelKey{}, nil, err
	}
	if credential.Type == dm.PoolTypeOAuth {
		lease.Release()
		return nil, dm.ChannelKey{}, nil, fmt.Errorf("media OAuth account protocol unsupported")
	}
	copy := *channel
	if account.BaseURL != "" {
		copy.BaseUrls = []dm.BaseUrl{{URL: account.BaseURL}}
	}
	if account.ProxyConfigID != nil {
		copy.ProxyConfigID = account.ProxyConfigID
		copy.ProxyMode = dm.ProxyUsageModePool
	}
	key := dm.ChannelKey{ID: account.ID, ChannelID: channel.ID, Enabled: true, ChannelKey: credential.EffectiveKeyWithExtra(account.Platform, account.GetExtra())}
	return &copy, key, lease, nil
}

func forwardMediaWithPoolHeaders(c *gin.Context, cfg mediaEndpointConfig, group dm.Group, channel *dm.Channel, key dm.ChannelKey, body []byte, requested, resolved string, stream bool, ctx context.Context, lease *poolscheduler.AccountLease) (int, error) {
	if lease == nil {
		return forwardMediaRequest(c, cfg, group, channel, key.ChannelKey, body, requested, resolved, stream, ctx)
	}
	defer lease.Release()
	account, credential, err := pool.CredentialsForRelay(lease.Account)
	if err != nil {
		return 0, err
	}
	copy := *channel
	copy.CustomHeader = append([]dm.CustomHeader(nil), channel.CustomHeader...)
	if credential.Type == dm.PoolTypeCookie {
		copy.CustomHeader = append(copy.CustomHeader, dm.CustomHeader{HeaderKey: "Cookie", HeaderValue: key.ChannelKey}, dm.CustomHeader{HeaderKey: "Authorization", HeaderValue: ""})
	}
	status, forwardErr := forwardMediaRequest(c, cfg, group, &copy, key.ChannelKey, body, requested, resolved, stream, ctx)
	if forwardErr == nil {
		poolscheduler.ReportResult(channel.PoolID, account.ID, true, 0, 0)
	} else if c.Request.Context().Err() == nil {
		poolscheduler.ReportResult(channel.PoolID, account.ID, false, 0, 0)
		if status == http.StatusTooManyRequests {
			poolscheduler.SetRateLimitCooldown(channel.PoolID, account.ID, time.Now().Add(poolBaseCooldown(channel.PoolID)))
		}
	}
	return status, forwardErr
}
