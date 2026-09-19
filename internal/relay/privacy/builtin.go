package privacy

import (
	"regexp"

	appmodel "github.com/lingyuins/octopus/internal/model"
)

// 内置检测正则。包级预编译，进程生命周期内只编译一次——
// DefaultMatchers 在每次中继请求上构建检测器列表，不能在此重复 Compile。
var (
	// 密钥/Token：常见前缀 + Bearer 头
	apiKeyRe = []*regexp.Regexp{
		regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),            // OpenAI 风格（含 sk-ant- 等变体）
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`),        // GitHub PAT
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                  // AWS AccessKeyID
		regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`),     // Slack token
		regexp.MustCompile(`Bearer\s+[A-Za-z0-9_\-\.=+/]{16,}`), // Authorization Bearer
	}

	// 账号密码等凭据写法：字段名 + 值（password=xxx / api_key: xxx 等）
	credentialRe = regexp.MustCompile(`(?i)(?:password|passwd|pwd|api[_-]?key|secret[_-]?key|access[_-]?key|token)\s*[:=]\s*["']?[^\s"',;]{4,}`)

	// 手机号：中国大陆 1xx 或国际 +号格式
	phoneRe = regexp.MustCompile(`(?:\+86[-\s]?)?1[3-9]\d{9}|\+\d{1,3}[-\s]?\d{7,12}`)

	// 邮箱：用 net/mail 的解析过于宽松/严格失衡，改用保守正则
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

	// 身份证：18 位（最后位可能是 X），校验位在 matcher 内验证
	idCardRe = regexp.MustCompile(`[1-9]\d{5}(?:19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]`)

	// 银行卡：13–19 位数字（Luhn 校验在 matcher 内做）
	bankCardRe = regexp.MustCompile(`[1-9]\d{12,18}`)

	// 高熵随机串：长度 20+ 的混合字符（熵计算在 matcher 内做）
	entropyRe = regexp.MustCompile(`[A-Za-z0-9+/=_\-]{20,}`)
)

// DefaultMatchers 返回内置检测器列表（不含自定义规则）。
// 顺序即 Run 输出顺序。检测器无状态，解析每次请求重建（成本仅为切片装配）。
func DefaultMatchers() []Matcher {
	matchers := make([]Matcher, 0, len(apiKeyRe)+7)
	for _, re := range apiKeyRe {
		matchers = append(matchers, &regexMatcher{cat: appmodel.PrivacyCategoryAPIKey, re: re})
	}
	matchers = append(matchers,
		&regexMatcher{cat: appmodel.PrivacyCategoryPassword, re: credentialRe},
		&regexMatcher{cat: appmodel.PrivacyCategoryPhone, re: phoneRe},
		&regexMatcher{cat: appmodel.PrivacyCategoryEmail, re: emailRe},
		&idCardMatcher{},
		&bankCardMatcher{},
		&entropyMatcher{},
	)
	return matchers
}