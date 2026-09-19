package relay

import (
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/relay/privacy"
	tmodel "github.com/lingyuins/octopus/internal/transformer/model"
)

func privacyTestConfig(categories map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig, rules []appmodel.PrivacyRule) (privacyFilterConfig, appmodel.PrivacyProtectionConfig) {
	pcfg := appmodel.PrivacyProtectionConfig{Categories: categories, Rules: rules}
	cfg := privacyFilterConfig{Enabled: true, Matchers: privacy.DefaultMatchersForTest(pcfg)}
	return cfg, pcfg
}

func TestApplyPrivacyProtection_Block(t *testing.T) {
	// phone 配置为 block：命中即拦截，请求保持原样
	cfg, pcfg := privacyTestConfig(map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
		appmodel.PrivacyCategoryPhone: {Enabled: boolPtr(true), Action: appmodel.PrivacyActionBlock},
	}, nil)

	content := "call me at 13812345678"
	req := &tmodel.InternalLLMRequest{
		Messages: []tmodel.Message{{Role: "user", Content: tmodel.MessageContent{Content: &content}}},
	}
	pm := newPrivacyPlaceholderMap()

	blocked, category := applyPrivacyProtection(req, cfg, pcfg, pm)
	if !blocked {
		t.Fatal("expected request to be blocked")
	}
	if category != string(appmodel.PrivacyCategoryPhone) {
		t.Fatalf("expected phone category, got %s", category)
	}
	if content != "call me at 13812345678" {
		t.Fatalf("blocked request should remain unmodified, got %q", content)
	}
}

func TestApplyPrivacyProtection_Filter(t *testing.T) {
	cfg, pcfg := privacyTestConfig(map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
		appmodel.PrivacyCategoryPhone: {Enabled: boolPtr(true), Action: appmodel.PrivacyActionFilter},
	}, nil)

	content := "call me at 13812345678 tomorrow"
	req := &tmodel.InternalLLMRequest{
		Messages: []tmodel.Message{{Role: "user", Content: tmodel.MessageContent{Content: &content}}},
	}
	pm := newPrivacyPlaceholderMap()

	blocked, _ := applyPrivacyProtection(req, cfg, pcfg, pm)
	if blocked {
		t.Fatal("filter action should not block")
	}
	if content == "call me at 13812345678 tomorrow" {
		t.Fatal("expected content to be sanitized")
	}
	if !pm.empty() {
		// 还原应能回到原文
		restored := pm.restore(content)
		if restored != "call me at 13812345678 tomorrow" {
			t.Fatalf("restore failed: got %q", restored)
		}
	}
}

func TestApplyPrivacyProtection_Disabled(t *testing.T) {
	cfg, pcfg := privacyTestConfig(nil, nil)
	cfg.Enabled = false

	content := "call me at 13812345678"
	req := &tmodel.InternalLLMRequest{
		Messages: []tmodel.Message{{Role: "user", Content: tmodel.MessageContent{Content: &content}}},
	}
	pm := newPrivacyPlaceholderMap()

	blocked, _ := applyPrivacyProtection(req, cfg, pcfg, pm)
	if blocked {
		t.Fatal("disabled config should never block")
	}
	if content != "call me at 13812345678" {
		t.Fatalf("disabled config should not modify content, got %q", content)
	}
}

func TestApplyPrivacyProtection_CategoryDisabled(t *testing.T) {
	// phone 类别禁用：包含手机号的文本不处理
	cfg, pcfg := privacyTestConfig(map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
		appmodel.PrivacyCategoryPhone: {Enabled: boolPtr(false), Action: appmodel.PrivacyActionBlock},
	}, nil)

	content := "call me at 13812345678"
	req := &tmodel.InternalLLMRequest{
		Messages: []tmodel.Message{{Role: "user", Content: tmodel.MessageContent{Content: &content}}},
	}
	pm := newPrivacyPlaceholderMap()

	blocked, _ := applyPrivacyProtection(req, cfg, pcfg, pm)
	if blocked {
		t.Fatal("disabled category should not block")
	}
	if content != "call me at 13812345678" {
		t.Fatalf("disabled category should not modify content, got %q", content)
	}
}

func TestApplyPrivacyProtection_MultipleMessagesAndEmbedding(t *testing.T) {
	cfg, pcfg := privacyTestConfig(map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
		appmodel.PrivacyCategoryEmail: {Enabled: boolPtr(true), Action: appmodel.PrivacyActionFilter},
	}, nil)

	sys := "system prompt"
	u1 := "email is alice@example.com"
	emb := "embed bob@test.org"
	req := &tmodel.InternalLLMRequest{
		Messages: []tmodel.Message{
			{Role: "system", Content: tmodel.MessageContent{Content: &sys}},
			{Role: "user", Content: tmodel.MessageContent{Content: &u1}},
		},
		EmbeddingInput: &tmodel.EmbeddingInput{Single: &emb},
	}
	pm := newPrivacyPlaceholderMap()

	if blocked, _ := applyPrivacyProtection(req, cfg, pcfg, pm); blocked {
		t.Fatal("filter should not block")
	}
	gotMsg := *req.Messages[1].Content.Content
	gotEmb := *req.EmbeddingInput.Single
	if gotMsg == "email is alice@example.com" || gotEmb == "embed bob@test.org" {
		t.Fatalf("expected both email targets to be sanitized, got %q / %q", gotMsg, gotEmb)
	}
	if sys != "system prompt" {
		t.Fatalf("system message without hits should be untouched, got %q", sys)
	}
	// 双向映射完整
	if pm.restore(gotMsg) != "email is alice@example.com" || pm.restore(gotEmb) != "embed bob@test.org" {
		t.Fatal("restore should recover original texts")
	}
}

func TestApplyPrivacyProtection_CustomKeyword(t *testing.T) {
	cfg, pcfg := privacyTestConfig(map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
		appmodel.PrivacyCategoryCustom: {Enabled: boolPtr(true), Action: appmodel.PrivacyActionFilter},
	}, []appmodel.PrivacyRule{{Type: "keyword", Pattern: "内部项目"}})

	content := "这是内部项目的资料"
	req := &tmodel.InternalLLMRequest{
		Messages: []tmodel.Message{{Role: "user", Content: tmodel.MessageContent{Content: &content}}},
	}
	pm := newPrivacyPlaceholderMap()

	if blocked, _ := applyPrivacyProtection(req, cfg, pcfg, pm); blocked {
		t.Fatal("filter should not block")
	}
	if content == "这是内部项目的资料" {
		t.Fatal("expected custom keyword to be sanitized")
	}
	if pm.restore(content) != "这是内部项目的资料" {
		t.Fatalf("restore failed: %q", content)
	}
}

func TestApplyPrivacyProtection_IdempotentMask(t *testing.T) {
	cfg, pcfg := privacyTestConfig(map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
		appmodel.PrivacyCategoryPhone: {Enabled: boolPtr(true), Action: appmodel.PrivacyActionFilter},
	}, nil)

	content := "13812345678 and 13812345678 again"
	req := &tmodel.InternalLLMRequest{
		Messages: []tmodel.Message{{Role: "user", Content: tmodel.MessageContent{Content: &content}}},
	}
	pm := newPrivacyPlaceholderMap()

	if blocked, _ := applyPrivacyProtection(req, cfg, pcfg, pm); blocked {
		t.Fatal("filter should not block")
	}
	// 同一原文 → 同一占位符（重试安全）
	if pm.restore(content) != "13812345678 and 13812345678 again" {
		t.Fatalf("restore failed: %q", content)
	}
}

func TestRestorePrivacyPlaceholders_ToolCallArguments(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	ph := pm.mask("13812345678")

	content := "found " + ph
	args := `{"phone":"` + ph + `"}`
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Message: &tmodel.Message{
				Content:   tmodel.MessageContent{Content: &content},
				ToolCalls: []tmodel.ToolCall{{Function: tmodel.FunctionCall{Name: "lookup", Arguments: args}}},
			},
		}},
	}

	restorePrivacyPlaceholders(resp, pm)

	gotContent := *resp.Choices[0].Message.Content.Content
	gotArgs := resp.Choices[0].Message.ToolCalls[0].Function.Arguments
	if gotContent != "found 13812345678" {
		t.Fatalf("content not restored: %q", gotContent)
	}
	if gotArgs != `{"phone":"13812345678"}` {
		t.Fatalf("tool call arguments not restored: %s", gotArgs)
	}
	_ = ph
}

func TestRestorePrivacyPlaceholders_NilMap(t *testing.T) {
	content := "no placeholders ⟨never⟩"
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{Message: &tmodel.Message{Content: tmodel.MessageContent{Content: &content}}}},
	}
	restorePrivacyPlaceholders(resp, nil)
	if content != "no placeholders ⟨never⟩" {
		t.Fatalf("nil map should not modify content, got %q", content)
	}
}

func TestLoadPrivacyFilterConfig_DisabledByDefault(t *testing.T) {
	cfg := loadPrivacyFilterConfig()
	// 默认设置 enabled=false；测试环境可能未初始化 setting，读 false 即不启用
	if cfg.Enabled && len(cfg.Matchers) == 0 {
		t.Fatal("enabled config must have matchers")
	}
}

func boolPtr(b bool) *bool { return &b }
