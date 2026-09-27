package privacy

import (
	"math"
	"strings"

	appmodel "github.com/lingyuins/octopus/internal/model"
)

// --- 身份证（GB 11643-1999 校验位） ---

type idCardMatcher struct{}

func (m *idCardMatcher) Category() appmodel.PrivacyCategory { return appmodel.PrivacyCategoryIDCard }

// idCardWeights 身份证第 18 位校验权重（Wi，模 11 加权）
var idCardWeights = [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}

// idCardCheckCodes 校验码表（余数 → 校验位）
var idCardCheckCodes = [11]byte{'1', '0', 'X', '9', '8', '7', '6', '5', '4', '3', '2'}

func validIDCardNumber(s string) bool {
	if len(s) != 18 {
		return false
	}
	sum := 0
	for i := 0; i < 17; i++ {
		d := int(s[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		sum += d * idCardWeights[i]
	}
	check := s[17]
	if check == 'x' {
		check = 'X'
	}
	return check == idCardCheckCodes[sum%11]
}

func (m *idCardMatcher) Find(text string) []Match {
	locs := idCardRe.FindAllIndex([]byte(text), -1)
	matches := make([]Match, 0, len(locs))
	for _, loc := range locs {
		if !validIDCardNumber(text[loc[0]:loc[1]]) {
			continue
		}
		matches = append(matches, Match{
			Category: appmodel.PrivacyCategoryIDCard,
			Value:    text[loc[0]:loc[1]],
			Start:    loc[0],
			End:      loc[1],
		})
	}
	return matches
}

// --- 银行卡（Luhn） ---

type bankCardMatcher struct{}

func (m *bankCardMatcher) Category() appmodel.PrivacyCategory {
	return appmodel.PrivacyCategoryBankCard
}

// validLuhn 标准信用卡 Luhn 校验：从右起偶数位×2、>9 减 9，总和模 10 为 0。
func validLuhn(s string) bool {
	sum := 0
	alt := false
	for i := len(s) - 1; i >= 0; i-- {
		d := int(s[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

func (m *bankCardMatcher) Find(text string) []Match {
	locs := bankCardRe.FindAllIndex([]byte(text), -1)
	matches := make([]Match, 0, len(locs))
	for _, loc := range locs {
		if !validLuhn(text[loc[0]:loc[1]]) {
			continue
		}
		matches = append(matches, Match{
			Category: appmodel.PrivacyCategoryBankCard,
			Value:    text[loc[0]:loc[1]],
			Start:    loc[0],
			End:      loc[1],
		})
	}
	return matches
}

// --- 高熵随机串 ---

const (
	entropyMinLength = 24  // 最短长度，低于此不算
	entropyThreshold = 4.2 // Shannon 熵阈值（bits/char），超过判定为高熵
	// entropyMaxSeparatorSegments 用分隔符（/、-、_、.）切出的词段数上限：
	// 超过即认为这是路径 / 分支名 / 驼峰包名等人类可读标识符，而非随机串。
	// 真随机串几乎不会包含这么多的分隔符（密钥字符集通常无结构）。
	entropyMaxSeparatorSegments = 3
	// entropyMinUniqueRatio 唯一字符占比下限：随机串字符重复率低（唯一字符接近总长），
	// 而自然语言标识符重复率高（multiplatformexchangeapi 大量重复）。
	entropyMinUniqueRatio = 0.6
)

type entropyMatcher struct{}

func (m *entropyMatcher) Category() appmodel.PrivacyCategory { return appmodel.PrivacyCategoryEntropy }

// shannonEntropy 计算字符串的字符级 Shannon 熵（bits/char）。
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	counts := make(map[byte]int)
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	var entropy float64
	for _, c := range counts {
		p := float64(c) / float64(len(s))
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// looksLikeHumanIdentifier 判定字符串是否更像路径 / 分支名 / 包名等人类可读标识符。
// 信号：分隔符切出的词段多（/e/workspace/idea/kotlin_demo 有 4 段），
// 或唯一字符占比低（multiplatformexchangeapi 大量重复字母）。
func looksLikeHumanIdentifier(s string) bool {
	separators := strings.IndexAny(s, "/-_.")
	if separators >= 0 {
		segments := 1
		for i := 0; i < len(s); i++ {
			if strings.ContainsRune("/-_.", rune(s[i])) {
				segments++
			}
		}
		if segments > entropyMaxSeparatorSegments {
			return true
		}
	}
	unique := make(map[byte]struct{})
	for i := 0; i < len(s); i++ {
		unique[s[i]] = struct{}{}
	}
	if float64(len(unique))/float64(len(s)) < entropyMinUniqueRatio {
		return true
	}
	return false
}

func (m *entropyMatcher) Find(text string) []Match {
	locs := entropyRe.FindAllIndex([]byte(text), -1)
	matches := make([]Match, 0, len(locs))
	for _, loc := range locs {
		value := text[loc[0]:loc[1]]
		// 长度截断：过长（如 base64 图片头、长 URL 片段）误报概率高且还原收益低
		if len(value) > 64 {
			continue
		}
		if len(value) < entropyMinLength {
			continue
		}
		// 纯数字串走银行卡/身份证检测，熵检测跳过避免误报
		if isAllDigits(value) {
			continue
		}
		// 路径 / 分支名 / 重复字符多的标识符不是随机串
		if looksLikeHumanIdentifier(value) {
			continue
		}
		if shannonEntropy(value) < entropyThreshold {
			continue
		}
		matches = append(matches, Match{
			Category: appmodel.PrivacyCategoryEntropy,
			Value:    value,
			Start:    loc[0],
			End:      loc[1],
		})
	}
	return matches
}

func isAllDigits(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) == -1
}
