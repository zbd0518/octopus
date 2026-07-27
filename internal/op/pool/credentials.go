package pool

import (
	"encoding/json"
	"strings"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

// EncryptCredentials 加密凭据 JSON 字符串。空字符串原样返回。
// crypto 未初始化时（ErrNoKey）返回原值（保持明文兼容，与 hub 一致策略）。
func EncryptCredentials(raw string) string {
	if raw == "" {
		return raw
	}
	enc, err := crypto.Encrypt(raw)
	if err != nil {
		// crypto 未初始化或加密失败：保持明文，避免阻断写入。
		// 首次 UpdateAccount（crypto 已初始化）时会加密落盘。
		return raw
	}
	return enc
}

// DecryptAccountCredentials 解密账号凭据，原地填充 acct.Credentials 为明文。
// 解密失败（非 enc: 前缀的明文）原样保留。调用方在返回账号给前端前应改用
// MaskAccountCredentials 脱敏，仅在需要明文（如转发、刷新）时调用本函数。
func DecryptAccountCredentials(acct *model.PoolAccount) error {
	if acct.Credentials == "" {
		return nil
	}
	plain, err := crypto.Decrypt(acct.Credentials)
	if err != nil {
		// 非 enc: 前缀的明文直接返回 nil（兼容存量）。
		return nil
	}
	acct.Credentials = plain
	return nil
}

// DecryptAccountsCredentials 批量解密。
func DecryptAccountsAccounts(accounts []model.PoolAccount) {
	for i := range accounts {
		_ = DecryptAccountCredentials(&accounts[i])
	}
}

// MaskAccountCredentials 将账号凭据脱敏后返回。
// 保留 type/platform 结构信息，敏感字段（token/api_key/cookie/access_token/
// refresh_token/id_token）替换为 "***"。返回 JSON 字符串。
func MaskAccountCredentials(acct *model.PoolAccount) string {
	if acct.Credentials == "" {
		return ""
	}
	// 先解密得到明文。
	plain := acct.Credentials
	if crypto.IsEncrypted(plain) {
		if decrypted, err := crypto.Decrypt(plain); err == nil {
			plain = decrypted
		}
	}
	// 解析为 map 脱敏。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plain), &raw); err != nil {
		// 非 JSON（如旧 cookie 明文）：整体脱敏。
		return "***"
	}
	sensitiveKeys := []string{"token", "api_key", "cookie", "access_token", "refresh_token", "id_token"}
	for _, k := range sensitiveKeys {
		if _, ok := raw[k]; ok {
			raw[k] = json.RawMessage(`"***"`)
		}
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return "***"
	}
	return string(out)
}

// MaskAccount 返回账号的脱敏副本（凭据字段替换为脱敏 JSON）。
// 供 handler list/detail 返回前端使用。
func MaskAccount(acct *model.PoolAccount) model.PoolAccount {
	cp := *acct
	cp.Credentials = MaskAccountCredentials(acct)
	// quota 字段不脱敏（额度快照无敏感信息）。
	return cp
}

// MaskAccounts 批量脱敏。
func MaskAccounts(accounts []model.PoolAccount) []model.PoolAccount {
	result := make([]model.PoolAccount, len(accounts))
	for i := range accounts {
		result[i] = MaskAccount(&accounts[i])
	}
	return result
}

// ParseImportedAccounts 解析批量导入的 JSON 数组为 PoolAccount 列表。
// 每个元素需至少包含 credentials（JSON 字符串或对象）字段；platform/type 可选，
// 缺省分别为 custom/apikey。credentials 对象会被加密后存入。
func ParseImportedAccounts(raw string, poolID int) ([]model.PoolAccount, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// 支持数组或单对象。
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		// 尝试单对象。
		var single json.RawMessage
		if err2 := json.Unmarshal([]byte(raw), &single); err2 == nil {
			arr = []json.RawMessage{single}
		} else {
			return nil, err
		}
	}
	result := make([]model.PoolAccount, 0, len(arr))
	for _, item := range arr {
		var acct model.PoolAccount
		acct.PoolID = poolID
		if acct.Status == "" {
			acct.Status = "active"
		}
		// 先解析到临时结构以提取 credentials（可能是对象或字符串）。
		var tmp struct {
			Name          string          `json:"name"`
			Platform      string          `json:"platform"`
			Type          string          `json:"type"`
			Models        string          `json:"models"`
			Credentials   json.RawMessage `json:"credentials"`
			BaseURL       string          `json:"base_url"`
			Notes         string          `json:"notes"`
			Priority      *int            `json:"priority"`
			Concurrency   *int            `json:"concurrency"`
			ProxyConfigID *int            `json:"proxy_config_id"`
		}
		if err := json.Unmarshal(item, &tmp); err != nil {
			return nil, err
		}
		acct.Name = tmp.Name
		acct.Platform = tmp.Platform
		if acct.Platform == "" {
			acct.Platform = model.PoolPlatformCustom
		}
		acct.Type = tmp.Type
		if acct.Type == "" {
			acct.Type = model.PoolTypeAPIKey
		}
		acct.Models = tmp.Models
		acct.BaseURL = tmp.BaseURL
		acct.Notes = tmp.Notes
		if tmp.Priority != nil {
			acct.Priority = *tmp.Priority
		}
		if tmp.Concurrency != nil {
			acct.Concurrency = *tmp.Concurrency
		}
		if tmp.ProxyConfigID != nil {
			id := *tmp.ProxyConfigID
			acct.ProxyConfigID = &id
		}
		// credentials：对象则序列化为 JSON 字符串，字符串则原样使用。
		credStr := ""
		if len(tmp.Credentials) > 0 {
			s := strings.TrimSpace(string(tmp.Credentials))
			if strings.HasPrefix(s, "{") {
				credStr = s
			} else if strings.HasPrefix(s, `"`) {
				_ = json.Unmarshal(tmp.Credentials, &credStr)
			} else {
				credStr = s
			}
		}
		acct.Credentials = EncryptCredentials(credStr)
		result = append(result, acct)
	}
	return result, nil
}
