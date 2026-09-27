package model

import (
	"strconv"
	"testing"
)

// 缺陷4 回归：circuit_breaker_cooldown 曾是全仓唯一一个「前端有 UI、常量已声明、
// 整数校验白名单已登记，但 DefaultSettings 没有 seed 行」的 key（三缺一）。
//
// 后果链：op/setting 的 SetString/SetInt 都是「缓存未命中即拒」（返回
// "setting not found"），而缓存只由 RefreshCache 从「DB 行 ∪ DefaultSettings
// 缺失项」填充 → 该 key 永远写不进去 → handlers/setting.go 映射成
// resp.InternalError = HTTP 500；前端 CircuitBreaker.tsx 的 settings.find(...)
// 得 undefined → 输入框恒空白、保存必失败；运行时 balancer/circuit.go 的
// GetCooldown 因 GetInt 报错而恒用兜底值 60。
//
// 修复只需补一行 seed：RefreshCache 会为存量库自动 CreateInBatches 补行，
// 无需数据库迁移。
func TestDefaultSettingsContainsCircuitBreakerCooldown(t *testing.T) {
	defaults := DefaultSettings()

	var (
		found bool
		value string
		count int
	)
	for _, s := range defaults {
		if s.Key == SettingKeyCircuitBreakerCooldown {
			count++
			found = true
			value = s.Value
		}
	}

	if !found {
		t.Fatalf("DefaultSettings() 缺少 %q 的 seed 行；"+
			"该 key 已在常量表与整数校验白名单中登记，缺失会让前端设置面板恒空白、保存返回 HTTP 500",
			SettingKeyCircuitBreakerCooldown)
	}
	if count != 1 {
		t.Fatalf("DefaultSettings() 含 %d 条 %q，want 1（重复 seed 会让 CreateInBatches 撞主键）",
			count, SettingKeyCircuitBreakerCooldown)
	}

	// seed 值必须与 balancer/circuit.go GetCooldown 的兜底值一致，否则补行本身
	// 就是一次行为变更（存量部署的冷却时间会突然从 60 变成别的值）。
	const wantCooldown = 60
	got, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("circuit_breaker_cooldown seed = %q，不是合法整数：%v", value, err)
	}
	if got != wantCooldown {
		t.Fatalf("circuit_breaker_cooldown seed = %d，want %d（必须与 balancer/circuit.go 的代码兜底值一致，避免行为变化）",
			got, wantCooldown)
	}
}

// 同一族的三个兄弟 key 都必须在场：它们是「三缺一」的对照组，
// 也是当初漏掉 cooldown 时最容易一起被误改的地方。
func TestDefaultSettingsCircuitBreakerFamilyComplete(t *testing.T) {
	want := map[SettingKey]string{
		SettingKeyCircuitBreakerThreshold:            "5",
		SettingKeyCircuitBreakerCooldown:             "60",
		SettingKeyCircuitBreakerMaxCooldown:          "600",
		SettingKeyCircuitBreakerHalfOpenProbeTimeout: "60",
	}

	got := make(map[SettingKey]string, len(want))
	for _, s := range DefaultSettings() {
		if _, ok := want[s.Key]; ok {
			got[s.Key] = s.Value
		}
	}

	for key, wantValue := range want {
		value, ok := got[key]
		if !ok {
			t.Errorf("DefaultSettings() 缺少 %q", key)
			continue
		}
		if value != wantValue {
			t.Errorf("%q seed = %q，want %q", key, value, wantValue)
		}
	}
}

// 补上的 seed 必须能通过 Setting 自身的校验（白名单里的整数校验路径），
// 否则 RefreshCache 补行后写入仍会被 Validate 拦下。
//
// 注意 Validate 对 circuit_breaker_cooldown 只做「必须是整数」校验，
// 并没有 < 0 的下界检查（不在那几个带专属下界的 key 里），所以负数能通过
// 校验，但运行时会因 balancer/circuit.go GetCooldown 的 `base <= 0` 判定而
// 回退到代码兜底值 60。这里如实断言现状，不把「应该加下界」写进测试。
func TestSettingValidateCircuitBreakerCooldown(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{value: "60", wantErr: false},
		{value: "30", wantErr: false},
		{value: "0", wantErr: false},
		{value: "600", wantErr: false},
		// Validate 对该 key 无下界检查，负数仍视为合法整数（见上方注释）。
		{value: "-1", wantErr: false},
		{value: "abc", wantErr: true},
	}

	for _, tt := range tests {
		s := Setting{Key: SettingKeyCircuitBreakerCooldown, Value: tt.value}
		err := s.Validate()
		if tt.wantErr && err == nil {
			t.Errorf("Validate(%q) = nil, want error", tt.value)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("Validate(%q) = %v, want nil", tt.value, err)
		}
	}
}
