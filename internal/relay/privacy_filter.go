package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	appmodel "github.com/lingyuins/octopus/internal/model"
	stg "github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/relay/privacy"
	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/utils/log"
)

var errPrivacyBlocked = errors.New("privacy protection blocked the request")

// privacyFilterConfig 请求侧隐私保护配置（每次中继请求加载一次并缓存）。
type privacyFilterConfig struct {
	Enabled  bool
	LogHits  bool // 是否打印命中明细调试日志（block/filter/还原），调试用
	Matchers []privacy.Matcher
}

// loadPrivacyFilterConfig 读取隐私保护设置并构建检测器列表。
func loadPrivacyFilterConfig() privacyFilterConfig {
	cfg := privacyFilterConfig{}

	logEnabled, _ := stg.GetBool(appmodel.SettingKeyPrivacyProtectionLogEnabled)
	cfg.LogHits = logEnabled

	enabled, _ := stg.GetBool(appmodel.SettingKeyPrivacyProtectionEnabled)
	if !enabled {
		return cfg
	}
	cfg.Enabled = true

	raw, _ := stg.GetString(appmodel.SettingKeyPrivacyProtectionConfig)
	var pcfg appmodel.PrivacyProtectionConfig
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &pcfg)
	}

	matchers := privacy.DefaultMatchers()
	matchers = append(matchers, privacy.CustomMatchers(pcfg.Rules)...)

	// 类别开关与动作过滤：禁用的类别不参与检测。
	enabledMatchers := make([]privacy.Matcher, 0, len(matchers))
	for _, m := range matchers {
		if pcfg.CategoryEnabled(m.Category()) {
			enabledMatchers = append(enabledMatchers, m)
		}
	}
	cfg.Matchers = enabledMatchers
	return cfg
}

// --- 占位符映射表 ---

// privacyPlaceholderMap 维护「原文 ↔ 占位符」的双向映射（请求级，跨重试共享）。
type privacyPlaceholderMap struct {
	mu        sync.Mutex
	forward   map[string]string // 原文 → 占位符
	reverse   map[string]string // 占位符 → 原文
	nextIndex int
	logHits   bool // 是否打印命中明细调试日志（随配置传入）
}

func newPrivacyPlaceholderMap() *privacyPlaceholderMap {
	return &privacyPlaceholderMap{
		forward: make(map[string]string),
		reverse: make(map[string]string),
	}
}

const privacyPlaceholderPrefix = "⟪PII-"

// mask 把原文替换为占位符；同一原文幂等返回同一占位符。
func (pm *privacyPlaceholderMap) mask(original string) string {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if ph, ok := pm.forward[original]; ok {
		return ph
	}
	pm.nextIndex++
	ph := fmt.Sprintf("%s%d⟫", privacyPlaceholderPrefix, pm.nextIndex)
	pm.forward[original] = ph
	pm.reverse[ph] = original
	return ph
}

// restore 把文本中的占位符还原成原文；无映射时原样返回。
// 日志开关打开时，每次实际还原都会打调试日志（占位符 → 原文）。
func (pm *privacyPlaceholderMap) restore(text string) string {
	if text == "" || !strings.Contains(text, privacyPlaceholderPrefix) {
		return text
	}
	// 持锁期间只做替换并收集命中，日志放到解锁后写，避免 I/O 阻塞其它请求。
	type restored struct{ placeholder, original string }
	var restoredPairs []restored

	pm.mu.Lock()
	result := text
	for ph, original := range pm.reverse {
		if strings.Contains(result, ph) {
			restoredPairs = append(restoredPairs, restored{ph, original})
			result = strings.ReplaceAll(result, ph, original)
		}
	}
	pm.mu.Unlock()

	if pm.logHits {
		for _, pair := range restoredPairs {
			log.Infof("[隐私保护] 响应还原: %q → %q", pair.placeholder, pair.original)
		}
	}
	return result
}

// empty 表示尚无任何脱敏记录。
func (pm *privacyPlaceholderMap) empty() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.forward) == 0
}

// maskRawBody 把原始请求 body 中的已记录命中片段替换为占位符（passthrough/raw 用）。
// 与 mask 不同：只替换已知片段，不新增映射（检测在 internalRequest 上已完成）。
func (pm *privacyPlaceholderMap) maskRawBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if len(pm.forward) == 0 {
		return body
	}
	result := string(body)
	for original, ph := range pm.forward {
		if strings.Contains(result, original) {
			result = strings.ReplaceAll(result, original, ph)
		}
	}
	return []byte(result)
}

// --- 请求文本提取与脱敏 ---

// extractRequestTexts 收集 InternalLLMRequest 中全部可见文本及其回写入口。
// 与响应侧 extractResponseText 不同，请求需要把替换结果写回消息结构。
func collectPrivacyTargets(req *model.InternalLLMRequest) []*string {
	if req == nil {
		return nil
	}
	var targets []*string
	for i := range req.Messages {
		msg := &req.Messages[i]
		if msg.Content.Content != nil {
			targets = append(targets, msg.Content.Content)
		}
		for j := range msg.Content.MultipleContent {
			part := &msg.Content.MultipleContent[j]
			if part.Type == "text" && part.Text != nil && *part.Text != "" {
				targets = append(targets, part.Text)
			}
		}
	}
	if req.EmbeddingInput != nil {
		if req.EmbeddingInput.Single != nil {
			targets = append(targets, req.EmbeddingInput.Single)
		}
		// EmbeddingInput.Multiple 为 []string，逐条处理
		for j := range req.EmbeddingInput.Multiple {
			targets = append(targets, &req.EmbeddingInput.Multiple[j])
		}
	}
	return targets
}

// applyPrivacyProtection 对请求做隐私处理（就地改写 req，调用方无需替换指针）：
//   - blocked=true：命中 block 类别，应直接拒绝请求。
//   - blocked=false：请求未被拦截；若发生脱敏，占位符映射已写入 pm。
//
// block 优先于 filter：只要任一启用类别为 block 且命中即拦截，请求保持原样。
func applyPrivacyProtection(req *model.InternalLLMRequest, cfg privacyFilterConfig, pcfg appmodel.PrivacyProtectionConfig, pm *privacyPlaceholderMap) (blocked bool, category string) {
	if !cfg.Enabled || req == nil || len(cfg.Matchers) == 0 {
		return false, ""
	}

	// block 类别优先：逐文本检测，命中即拦截（请求保持原样，调用方返回错误）。
	// block 检测完成后，再对 filter 类别做逐文本检测与替换。
	targets := collectPrivacyTargets(req)

	// 第一遍：block 类别检测（不修改请求）
	for _, p := range targets {
		if p == nil || *p == "" {
			continue
		}
		for _, hit := range privacy.Run(*p, cfg.Matchers) {
			if pcfg.CategoryAction(hit.Category) == appmodel.PrivacyActionBlock {
				// 调试日志（受隐私保护日志开关控制）：打印命中的类别与内容片段
				if cfg.LogHits {
					values := make([]string, 0, len(hit.Matches))
					for _, m := range hit.Matches {
						values = append(values, m.Value)
					}
					log.Infof("[隐私保护] block 命中: category=%s matches=%q", hit.Category, values)
				}
				return true, string(hit.Category)
			}
		}
	}

	// 第二遍：filter 类别脱敏（就地替换为占位符）
	for _, p := range targets {
		if p == nil || *p == "" {
			continue
		}
		text := *p
		for _, hit := range privacy.Run(text, cfg.Matchers) {
			if pcfg.CategoryAction(hit.Category) != appmodel.PrivacyActionFilter {
				continue
			}
			changed := false
			for _, m := range hit.Matches {
				if m.Value == "" {
					continue
				}
				placeholder := pm.mask(m.Value)
				if cfg.LogHits {
					log.Infof("[隐私保护] filter 命中并脱敏: category=%s %q → %q", hit.Category, m.Value, placeholder)
				}
				text = strings.ReplaceAll(text, m.Value, placeholder)
				changed = true
			}
			if changed {
				*p = text
			}
		}
	}
	return false, ""
}

// --- 响应还原 ---

// newStreamRestorer 为流式路径创建占位符还原缓冲；无脱敏时返回 nil（零开销）。
func (pm *privacyPlaceholderMap) newStreamRestorer() *privacyStreamRestorer {
	if pm == nil || pm.empty() {
		return nil
	}
	return newPrivacyStreamRestorer(pm)
}

// restorePrivacyPlaceholders 把响应文本中的占位符还原为原文。
func restorePrivacyPlaceholders(resp *model.InternalLLMResponse, pm *privacyPlaceholderMap) {
	if resp == nil || pm == nil || pm.empty() {
		return
	}
	for i := range resp.Choices {
		choice := &resp.Choices[i]
		var msg *model.Message
		if choice.Message != nil {
			msg = choice.Message
		} else if choice.Delta != nil {
			msg = choice.Delta
		}
		if msg == nil {
			continue
		}
		if msg.Content.Content != nil {
			*msg.Content.Content = pm.restore(*msg.Content.Content)
		}
		for j := range msg.Content.MultipleContent {
			part := &msg.Content.MultipleContent[j]
			if part.Type == "text" && part.Text != nil && *part.Text != "" {
				*part.Text = pm.restore(*part.Text)
			}
		}
		for j := range msg.ToolCalls {
			tc := &msg.ToolCalls[j]
			if tc.Function.Arguments != "" {
				tc.Function.Arguments = pm.restore(tc.Function.Arguments)
			}
		}
	}
}

// reqPrivacyRestore 占位：流式/非流式还原接入前的过渡（防止未使用告警）。
func reqPrivacyRestore(pm *privacyPlaceholderMap) {}
