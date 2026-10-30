package channel

import (
	"fmt"
	"math"
)

type keyUsageDelta struct {
	cost       float64
	statusCode int
}

// runtimeUpdateLock protects deltas together with their cache projection.
var pendingKeyUsage = make(map[int]keyUsageDelta)

func RecordKeyUsage(channelID, keyID int, cost float64, statusCode int, usedAt int64) error {
	runtimeUpdateLock.Lock()
	defer runtimeUpdateLock.Unlock()
	return recordKeyUsageLocked(channelID, keyID, cost, statusCode, usedAt)
}

func recordKeyUsageLocked(channelID, keyID int, cost float64, statusCode int, usedAt int64) error {
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return fmt.Errorf("invalid channel key cost")
	}
	key, exists := keyCache.Get(keyID)
	channel, channelExists := chCache.Get(channelID)
	if !exists || !channelExists || key.ChannelID != channelID || channel.PoolID != 0 {
		return fmt.Errorf("channel key not found")
	}
	keys := append(channel.Keys[:0:0], channel.Keys...)
	found := false
	for i := range keys {
		if keys[i].ID == keyID {
			key.TotalCost += cost
			if usedAt >= key.LastUseTimeStamp && statusCode > 0 {
				key.StatusCode = statusCode
			}
			key.LastUseTimeStamp = max(key.LastUseTimeStamp, usedAt)
			keys[i] = key
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("channel key not found")
	}
	channel.Keys = keys
	chCache.Set(channelID, channel)
	keyCache.Set(keyID, key)
	delta := pendingKeyUsage[keyID]
	delta.cost += cost
	delta.statusCode = key.StatusCode
	pendingKeyUsage[keyID] = delta
	keyCacheNeedUpdateLock.Lock()
	keyCacheNeedUpdate[keyID] = struct{}{}
	keyCacheNeedUpdateLock.Unlock()
	return nil
}
