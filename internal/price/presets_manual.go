package price

// 手工维护的价格预设，与 scripts/updatePrice.py 生成的 presets.go 分离，
// 避免重跑脚本时丢失条目。数值按 USD / 1M tokens 存储（与 presets.go 全部条目口径一致）。
//
// 来源：DeepSeek 官网价目表（2026-07，CNY / 1M tokens，按 ≈7.15 汇率换算为 USD）：
//   - deepseek-v4-flash：输入（缓存命中）0.02 元，输入（缓存未命中）1 元，输出 2 元
//   - deepseek-v4-pro：  输入（缓存命中）0.025 元，输入（缓存未命中）3 元，输出 6 元
//
// 无缓存写（CacheWrite=0）。models.dev 后台刷新收录后会用官方数据覆盖同名键，属预期。

import "github.com/lingyuins/octopus/internal/model"

func init() {
	llmPrice["deepseek-v4-flash"] = model.LLMPrice{Input: 0.14, Output: 0.28, CacheRead: 0.0028, CacheWrite: 0}
	llmPrice["deepseek-v4-pro"] = model.LLMPrice{Input: 0.42, Output: 0.84, CacheRead: 0.0035, CacheWrite: 0}
}
