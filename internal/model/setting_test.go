package model

import (
	"testing"
)

func TestSettingValidateRelayRetry(t *testing.T) {
	tests := []struct {
		name    string
		key     SettingKey
		value   string
		wantErr bool
	}{
		{name: "retry count zero allowed", key: SettingKeyRelayRetryCount, value: "0"},
		{name: "retry count positive allowed", key: SettingKeyRelayRetryCount, value: "3"},
		{name: "retry count negative rejected", key: SettingKeyRelayRetryCount, value: "-1", wantErr: true},
		{name: "route retries one allowed", key: SettingKeyRelayRouteRetries, value: "1"},
		{name: "route retries zero rejected", key: SettingKeyRelayRouteRetries, value: "0", wantErr: true},
		{name: "ratelimit cooldown zero allowed", key: SettingKeyRatelimitCooldown, value: "0"},
		{name: "ratelimit cooldown positive allowed", key: SettingKeyRatelimitCooldown, value: "300"},
		{name: "ratelimit cooldown negative rejected", key: SettingKeyRatelimitCooldown, value: "-1", wantErr: true},
		{name: "max total attempts zero allowed", key: SettingKeyRelayMaxTotalAttempts, value: "0"},
		{name: "max total attempts positive allowed", key: SettingKeyRelayMaxTotalAttempts, value: "5"},
		{name: "max total attempts negative rejected", key: SettingKeyRelayMaxTotalAttempts, value: "-1", wantErr: true},
		// 429 渠道内延时重试：开关默认关闭；间隔/总等待必须 >=1。
		{name: "rate limit hold interval one allowed", key: SettingKeyRateLimitHoldInterval, value: "10"},
		{name: "rate limit hold interval zero rejected", key: SettingKeyRateLimitHoldInterval, value: "0", wantErr: true},
		{name: "rate limit hold max wait one allowed", key: SettingKeyRateLimitHoldMaxWait, value: "60"},
		{name: "rate limit hold max wait zero rejected", key: SettingKeyRateLimitHoldMaxWait, value: "0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{Key: tt.key, Value: tt.value}
			err := setting.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestSettingValidateRateLimitHoldEnabled(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "true allowed", value: "true"},
		{name: "false allowed", value: "false"},
		{name: "invalid rejected", value: "yes", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{Key: SettingKeyRateLimitHoldEnabled, Value: tt.value}
			err := setting.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
