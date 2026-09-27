package privacy

import (
	"strings"
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
)

// collectHits 把 Run 结果压成「类别:值」串便于断言
func collectHits(t *testing.T, text string, matchers []Matcher) map[string][]string {
	t.Helper()
	hits := Run(text, matchers)
	result := make(map[string][]string)
	for _, hit := range hits {
		for _, m := range hit.Matches {
			result[string(hit.Category)] = append(result[string(hit.Category)], m.Value)
		}
	}
	return result
}

func TestRun_NilOnEmpty(t *testing.T) {
	if got := Run("", DefaultMatchers()); got != nil {
		t.Fatalf("expected nil hits for empty text, got %v", got)
	}
	if got := Run("hello", nil); got != nil {
		t.Fatalf("expected nil hits for no matchers, got %v", got)
	}
}

func TestAPIKeyMatcher(t *testing.T) {
	matchers := DefaultMatchers()
	tests := []struct {
		name    string
		text    string
		want    string
		notWant bool
	}{
		{name: "openai style", text: "use key sk-abcdefghij1234567890 to call", want: "sk-abcdefghij1234567890"},
		{name: "github pat", text: "token ghp_abcdefghijklmnopqrstuvwxyz123456 leaked", want: "ghp_abcdefghijklmnopqrstuvwxyz123456"},
		{name: "aws key", text: "AKIAIOSFODNN7EXAMPLE is amazon key", want: "AKIAIOSFODNN7EXAMPLE"},
		{name: "bearer", text: "Bearer abcdefghij1234567890ABCDEF", want: "Bearer abcdefghij1234567890ABCDEF"},
		{name: "plain text no match", text: "just a normal sentence about keys", notWant: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectHits(t, tt.text, matchers)
			values := got[string(appmodel.PrivacyCategoryAPIKey)]
			if tt.notWant {
				if len(values) != 0 {
					t.Fatalf("expected no api_key hit, got %v", values)
				}
				return
			}
			found := false
			for _, v := range values {
				if strings.Contains(v, tt.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected api_key hit containing %q, got %v", tt.want, values)
			}
		})
	}
}

func TestCredentialMatcher(t *testing.T) {
	got := collectHits(t, `password=supersecret123 and api_key: "abcd1234efgh"`, DefaultMatchers())
	values := got[string(appmodel.PrivacyCategoryPassword)]
	if len(values) == 0 {
		t.Fatal("expected password hits, got none")
	}
	joined := strings.Join(values, "|")
	if !strings.Contains(joined, "supersecret123") {
		t.Fatalf("expected password value hit, got %v", values)
	}
	if !strings.Contains(joined, "abcd1234efgh") {
		t.Fatalf("expected api_key value hit, got %v", values)
	}

	noHit := collectHits(t, "the password policy requires rotation", DefaultMatchers())
	if v := noHit[string(appmodel.PrivacyCategoryPassword)]; len(v) != 0 {
		t.Fatalf("expected no credential hit without =/: value, got %v", v)
	}
}

func TestPhoneMatcher(t *testing.T) {
	got := collectHits(t, "call me at 13812345678 or +8613912345678", DefaultMatchers())
	values := got[string(appmodel.PrivacyCategoryPhone)]
	if len(values) == 0 {
		t.Fatal("expected phone hits")
	}
	joined := strings.Join(values, "|")
	if !strings.Contains(joined, "13812345678") || !strings.Contains(joined, "13912345678") {
		t.Fatalf("expected both phone numbers, got %v", values)
	}
}

func TestEmailMatcher(t *testing.T) {
	got := collectHits(t, "contact alice@example.com or bob@test.org.cn", DefaultMatchers())
	values := got[string(appmodel.PrivacyCategoryEmail)]
	if len(values) != 2 {
		t.Fatalf("expected 2 email hits, got %v", values)
	}
}

func TestIDCardMatcher(t *testing.T) {
	// 110101199003077512 是校验位合法的测试号码（常用测试数据）
	valid := "110101199003077512"
	if !validIDCardNumber(valid) {
		t.Fatalf("test id %s expected valid", valid)
	}
	got := collectHits(t, "身份证号 "+valid+" 已登记", DefaultMatchers())
	values := got[string(appmodel.PrivacyCategoryIDCard)]
	if len(values) != 1 || values[0] != valid {
		t.Fatalf("expected id card hit %s, got %v", valid, values)
	}

	// 校验位不通过 → 不命中
	got2 := collectHits(t, "身份证号 110101199003077513 已登记", DefaultMatchers())
	if v := got2[string(appmodel.PrivacyCategoryIDCard)]; len(v) != 0 {
		t.Fatalf("expected checksum-failed id not to hit, got %v", v)
	}
}

func TestBankCardMatcher(t *testing.T) {
	// 4111111111111111 是 Luhn 合法的测试卡号
	valid := "4111111111111111"
	if !validLuhn(valid) {
		t.Fatalf("test card %s expected valid", valid)
	}
	got := collectHits(t, "卡号 "+valid+" 已绑定", DefaultMatchers())
	values := got[string(appmodel.PrivacyCategoryBankCard)]
	if len(values) != 1 || values[0] != valid {
		t.Fatalf("expected bank card hit %s, got %v", valid, values)
	}

	// Luhn 不过 → 不命中
	got2 := collectHits(t, "卡号 4111111111111112 已绑定", DefaultMatchers())
	if v := got2[string(appmodel.PrivacyCategoryBankCard)]; len(v) != 0 {
		t.Fatalf("expected luhn-failed card not to hit, got %v", v)
	}
}

func TestEntropyMatcher(t *testing.T) {
	// 高熵随机串命中：大小写混合 + 数字 + 符号，无分隔符结构，字符重复率低
	got := collectHits(t, "random string: aB3xK9mQ2pL7vN4wE8jR5tY1uI6oP0zH", DefaultMatchers())
	values := got[string(appmodel.PrivacyCategoryEntropy)]
	if len(values) == 0 {
		t.Fatal("expected entropy hit for high-entropy string")
	}

	noHit := collectHits(t, "this is a plain english sentence about things", DefaultMatchers())
	if v := noHit[string(appmodel.PrivacyCategoryEntropy)]; len(v) != 0 {
		t.Fatalf("expected no entropy hit for plain sentence, got %v", v)
	}
}

// 用户实测误报回归（issue 020 反馈）：文件路径 / 包名 / 分支名等人类可读标识符
// 不得被熵检测命中。
func TestEntropyMatcher_HumanIdentifiersNotMatched(t *testing.T) {
	text := "look at /e/workspace/idea/kotlin_demo and /client/exchange/MultiPlatformExchangeApi " +
		"also branch -o-feature-lx-sdk-sync-doc-baseline-20260917 and word multiplatformexchangeapi " +
		"plus /lx-sdk-20260917-field-diff/review-Finance and /lingxing/pipeline/MultiPlatformIngestor"
	got := collectHits(t, text, DefaultMatchers())
	if v := got[string(appmodel.PrivacyCategoryEntropy)]; len(v) != 0 {
		t.Fatalf("human-readable identifiers must not hit entropy detector, got %v", v)
	}
}

// 熵检测未配置时默认关闭（entropy 是唯一默认禁用的类别）。
func TestPrivacyCategoryEnabled_EntropyDefaultsOff(t *testing.T) {
	enabled := true
	cfg := appmodel.PrivacyProtectionConfig{}
	if cfg.CategoryEnabled(appmodel.PrivacyCategoryEntropy) {
		t.Fatal("entropy must default to disabled")
	}
	if !cfg.CategoryEnabled(appmodel.PrivacyCategoryPhone) {
		t.Fatal("other categories must default to enabled")
	}
	// 显式开启后应生效
	cfgExplicit := appmodel.PrivacyProtectionConfig{
		Categories: map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
			appmodel.PrivacyCategoryEntropy: {Enabled: &enabled},
		},
	}
	if !cfgExplicit.CategoryEnabled(appmodel.PrivacyCategoryEntropy) {
		t.Fatal("explicitly enabled entropy must be enabled")
	}
}

func TestCustomMatchers(t *testing.T) {
	rules := []appmodel.PrivacyRule{
		{Type: "keyword", Pattern: "内部项目"},
		{Type: "regex", Pattern: `octopus-[a-z]{3}\d{2}`},
	}
	matchers := CustomMatchers(rules)

	got := collectHits(t, "这是内部项目的代号 octopus-abc12", matchers)
	custom := got[string(appmodel.PrivacyCategoryCustom)]
	if len(custom) != 2 {
		t.Fatalf("expected 2 custom hits (keyword + regex), got %v", custom)
	}

	// keyword 大小写不敏感
	rules2 := []appmodel.PrivacyRule{{Type: "keyword", Pattern: "Secret"}}
	got2 := collectHits(t, "this is SECRET info", CustomMatchers(rules2))
	if v := got2[string(appmodel.PrivacyCategoryCustom)]; len(v) != 1 {
		t.Fatalf("expected case-insensitive keyword hit, got %v", v)
	}
}

func TestValidatePrivacyRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    appmodel.PrivacyRule
		wantErr bool
	}{
		{name: "valid keyword", rule: appmodel.PrivacyRule{Type: "keyword", Pattern: "foo"}, wantErr: false},
		{name: "valid regex", rule: appmodel.PrivacyRule{Type: "regex", Pattern: `foo\d+`}, wantErr: false},
		{name: "invalid regex", rule: appmodel.PrivacyRule{Type: "regex", Pattern: "foo["}, wantErr: true},
		{name: "empty pattern", rule: appmodel.PrivacyRule{Type: "keyword", Pattern: "  "}, wantErr: true},
		{name: "invalid type", rule: appmodel.PrivacyRule{Type: "glob", Pattern: "foo"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate(0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePrivacyProtectionConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "empty ok", raw: "", wantErr: false},
		{name: "valid", raw: `{"categories":{"phone":{"enabled":true,"action":"block"}},"rules":[{"type":"keyword","pattern":"foo"}]}`, wantErr: false},
		{name: "bad json", raw: `{`, wantErr: true},
		{name: "unknown category", raw: `{"categories":{"foo":{"enabled":true}}}`, wantErr: true},
		{name: "bad action", raw: `{"categories":{"phone":{"action":"replace"}}}`, wantErr: true},
		{name: "invalid rule regex", raw: `{"rules":[{"type":"regex","pattern":"["}]}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := appmodel.ValidatePrivacyProtectionConfig(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePrivacyProtectionConfig(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestPrivacyProtectionConfigDefaults(t *testing.T) {
	var cfg appmodel.PrivacyProtectionConfig
	if err := appmodel.ValidatePrivacyProtectionConfig(`{}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 未配置类别默认开启 + filter
	if !cfg.CategoryEnabled(appmodel.PrivacyCategoryPhone) {
		t.Fatal("expected default category enabled")
	}
	if cfg.CategoryAction(appmodel.PrivacyCategoryPhone) != appmodel.PrivacyActionFilter {
		t.Fatalf("expected default action filter, got %s", cfg.CategoryAction(appmodel.PrivacyCategoryPhone))
	}
}
