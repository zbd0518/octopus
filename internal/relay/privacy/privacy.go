// Package privacy 实现请求侧敏感信息检测（issue 020）。
// 每个类别一个检测器，接口统一为「文本 → 命中片段列表」；
// 正则编译一次并复用，检测器无状态、可并发调用。
package privacy

import (
	"regexp"

	appmodel "github.com/lingyuins/octopus/internal/model"
)

// Match 一次命中的敏感片段
type Match struct {
	Category appmodel.PrivacyCategory // 所属类别
	Value    string                   // 命中的原文片段
	Start    int                      // 在输入文本中的起始字节偏移
	End      int                      // 结束字节偏移（不含）
}

// Hit 单个类别的检测结果
type Hit struct {
	Category appmodel.PrivacyCategory
	Matches  []Match
}

// Matcher 单个类别的检测器
type Matcher interface {
	Category() appmodel.PrivacyCategory
	// Find 返回文本中的命中片段；实现必须无状态、可并发调用。
	Find(text string) []Match
}

// Run 对文本执行全部检测器，返回按类别聚合的命中结果（顺序与传入的 matchers 一致）。
func Run(text string, matchers []Matcher) []Hit {
	if text == "" || len(matchers) == 0 {
		return nil
	}
	var hits []Hit
	for _, m := range matchers {
		matches := m.Find(text)
		if len(matches) > 0 {
			hits = append(hits, Hit{Category: m.Category(), Matches: matches})
		}
	}
	return hits
}

// regexMatcher 通用正则检测器
type regexMatcher struct {
	cat appmodel.PrivacyCategory
	re  *regexp.Regexp
}

func (m *regexMatcher) Category() appmodel.PrivacyCategory { return m.cat }

func (m *regexMatcher) Find(text string) []Match {
	locs := m.re.FindAllIndex([]byte(text), -1)
	if len(locs) == 0 {
		return nil
	}
	matches := make([]Match, 0, len(locs))
	for _, loc := range locs {
		if loc[1] <= loc[0] {
			continue
		}
		matches = append(matches, Match{
			Category: m.cat,
			Value:    text[loc[0]:loc[1]],
			Start:    loc[0],
			End:      loc[1],
		})
	}
	return matches
}

// newRegexMatcher 编译失败返回 nil（配置层已校验，这里只防御内置正则）
func newRegexMatcher(cat appmodel.PrivacyCategory, pattern string) Matcher {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return &regexMatcher{cat: cat, re: re}
}
