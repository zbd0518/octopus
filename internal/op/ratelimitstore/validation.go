package ratelimitstore

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ValidateLimits(global Limits, raw string) error {
	if global.RPM < 0 || global.TPM < 0 {
		return fmt.Errorf("quota limits must be non-negative")
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var quotas map[string]Limits
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&quotas); err != nil || quotas == nil || !json.Valid([]byte(raw)) {
		return fmt.Errorf("invalid model quota object")
	}
	for name, limit := range quotas {
		if name == "" || name != strings.TrimSpace(name) || limit.RPM < 0 || limit.TPM < 0 {
			return fmt.Errorf("invalid model quota")
		}
	}
	return nil
}
