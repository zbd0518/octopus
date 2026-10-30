package apikey

import (
	"sync"
	"time"
)

var missingAPIKeys = struct {
	sync.Mutex
	entries map[string]time.Time
}{entries: make(map[string]time.Time)}

func missingAPIKey(hash string) bool {
	missingAPIKeys.Lock()
	defer missingAPIKeys.Unlock()
	until, exists := missingAPIKeys.entries[hash]
	if exists && time.Now().Before(until) {
		return true
	}
	delete(missingAPIKeys.entries, hash)
	return false
}

func rememberMissingAPIKey(hash string) {
	missingAPIKeys.Lock()
	defer missingAPIKeys.Unlock()
	if len(missingAPIKeys.entries) >= 4096 {
		clear(missingAPIKeys.entries)
	}
	missingAPIKeys.entries[hash] = time.Now().Add(5 * time.Second)
}

func forgetMissingAPIKey(raw string) {
	missingAPIKeys.Lock()
	delete(missingAPIKeys.entries, HashAPIKey(raw))
	missingAPIKeys.Unlock()
}
