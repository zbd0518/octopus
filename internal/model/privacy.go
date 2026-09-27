package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// 隐私保护检测类别（PrivacyCategory 取值即前端/存储使用的稳定标识）
type PrivacyCategory string

const (
	PrivacyCategoryAPIKey   PrivacyCategory = "api_key"   // 密钥 / Token（sk-、ghp_、AKIA、Bearer 等）
	PrivacyCategoryPassword PrivacyCategory = "password"  // 账号密码等凭据（password= / api_key: 等写法）
	PrivacyCategoryPhone    PrivacyCategory = "phone"     // 手机号
	PrivacyCategoryEmail    PrivacyCategory = "email"     // 邮箱
	PrivacyCategoryIDCard   PrivacyCategory = "id_card"   // 身份证号（含校验位验证）
	PrivacyCategoryBankCard PrivacyCategory = "bank_card" // 银行卡号（Luhn 校验）
	PrivacyCategoryEntropy  PrivacyCategory = "entropy"   // 高熵随机串
	PrivacyCategoryCustom   PrivacyCategory = "custom"    // 自定义敏感词/正则
)

func (c PrivacyCategory) IsValid() bool {
	switch c {
	case PrivacyCategoryAPIKey, PrivacyCategoryPassword, PrivacyCategoryPhone, PrivacyCategoryEmail,
		PrivacyCategoryIDCard, PrivacyCategoryBankCard, PrivacyCategoryEntropy, PrivacyCategoryCustom:
		return true
	}
	return false
}

// 隐私保护动作：block=拦截请求；filter=脱敏后转发（响应还原见 issue 020）
type PrivacyAction string

const (
	PrivacyActionBlock  PrivacyAction = "block"
	PrivacyActionFilter PrivacyAction = "filter"
)

func (a PrivacyAction) IsValid() bool {
	return a == PrivacyActionBlock || a == PrivacyActionFilter
}

// PrivacyRule 单条自定义敏感规则：字面关键词或正则
type PrivacyRule struct {
	Type    string `json:"type"`    // "keyword" | "regex"
	Pattern string `json:"pattern"` // 关键词文本或正则表达式
}

func (r PrivacyRule) Validate(index int) error {
	pattern := strings.TrimSpace(r.Pattern)
	if pattern == "" {
		return fmt.Errorf("privacy rule #%d pattern is required", index+1)
	}
	switch r.Type {
	case "keyword":
		return nil
	case "regex":
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("privacy rule #%d regex is invalid: %w", index+1, err)
		}
		return nil
	default:
		return fmt.Errorf("privacy rule #%d type must be keyword or regex", index+1)
	}
}

// PrivacyCategoryConfig 单个类别的配置
type PrivacyCategoryConfig struct {
	Enabled *bool         `json:"enabled,omitempty"`
	Action  PrivacyAction `json:"action,omitempty"`
}

func (c PrivacyCategoryConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// PrivacyProtectionConfig 隐私保护完整配置（privacy_protection_config 设置项的 JSON 结构）
type PrivacyProtectionConfig struct {
	Categories map[PrivacyCategory]PrivacyCategoryConfig `json:"categories,omitempty"`
	Rules      []PrivacyRule                             `json:"rules,omitempty"`
}

// CategoryEnabled 类别开关（总开关关闭时整体不生效）。
// 未配置时默认开启，唯独 entropy（高熵随机串）默认关闭：
// 熵检测本质是启发式，对文件路径 / 包名 / 分支名等人类可读标识符误报率高
// （如 /e/workspace/idea/kotlin_demo），用户显式开启后才参与检测。
func (c *PrivacyProtectionConfig) CategoryEnabled(cat PrivacyCategory) bool {
	if cfg, ok := c.Categories[cat]; ok {
		return cfg.IsEnabled()
	}
	return cat != PrivacyCategoryEntropy
}

// CategoryAction 类别动作，未配置默认 filter（脱敏比拦截温和，作为缺省）
func (c *PrivacyProtectionConfig) CategoryAction(cat PrivacyCategory) PrivacyAction {
	if cfg, ok := c.Categories[cat]; ok && cfg.Action.IsValid() {
		return cfg.Action
	}
	return PrivacyActionFilter
}

// ValidatePrivacyProtectionConfig 校验 privacy_protection_config 设置值
func ValidatePrivacyProtectionConfig(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var cfg PrivacyProtectionConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return fmt.Errorf("privacy protection config must be a valid JSON object")
	}

	for cat, cc := range cfg.Categories {
		if !cat.IsValid() {
			return fmt.Errorf("privacy protection config has unknown category %q", string(cat))
		}
		if cc.Action != "" && !cc.Action.IsValid() {
			return fmt.Errorf("privacy protection config category %q action must be block or filter", string(cat))
		}
	}

	for i, rule := range cfg.Rules {
		if err := rule.Validate(i); err != nil {
			return err
		}
	}

	return nil
}
