package privacy

import (
	appmodel "github.com/lingyuins/octopus/internal/model"
)

// DefaultMatchersForTest 供 relay 层测试使用：不做类别过滤，返回全部内置 + 自定义检测器。
func DefaultMatchersForTest(pcfg appmodel.PrivacyProtectionConfig) []Matcher {
	matchers := DefaultMatchers()
	matchers = append(matchers, CustomMatchers(pcfg.Rules)...)
	enabled := make([]Matcher, 0, len(matchers))
	for _, m := range matchers {
		if pcfg.CategoryEnabled(m.Category()) {
			enabled = append(enabled, m)
		}
	}
	return enabled
}
