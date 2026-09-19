package privacy

import (
	"strings"

	appmodel "github.com/lingyuins/octopus/internal/model"
)

// CustomMatchers 从用户自定义规则构建检测器（规则合法性已由设置校验保证）。
// keyword 规则大小写不敏感字面匹配；regex 规则按用户正则匹配。
// 编译失败/空 pattern 的规则跳过（防御）。
func CustomMatchers(rules []appmodel.PrivacyRule) []Matcher {
	var matchers []Matcher
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" {
			continue
		}
		switch rule.Type {
		case "keyword":
			matchers = append(matchers, &keywordMatcher{keyword: pattern})
		case "regex":
			if m := newRegexMatcher(appmodel.PrivacyCategoryCustom, pattern); m != nil {
				matchers = append(matchers, m)
			}
		}
	}
	return matchers
}

// keywordMatcher 字面关键词检测器（大小写不敏感，覆盖方式与输出拦截 findMatchedKeyword 一致）
type keywordMatcher struct {
	keyword string
}

func (m *keywordMatcher) Category() appmodel.PrivacyCategory { return appmodel.PrivacyCategoryCustom }

func (m *keywordMatcher) Find(text string) []Match {
	if m.keyword == "" || text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	kw := strings.ToLower(m.keyword)
	var matches []Match
	offset := 0
	for {
		idx := strings.Index(lower[offset:], kw)
		if idx < 0 {
			break
		}
		start := offset + idx
		end := start + len(kw)
		matches = append(matches, Match{
			Category: appmodel.PrivacyCategoryCustom,
			Value:    text[start:end],
			Start:    start,
			End:      end,
		})
		offset = end
	}
	return matches
}
