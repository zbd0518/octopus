package privacy

import (
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
)

// TestEntropyFalsePositivesFromUserLog 用用户实测日志中的全部误报样例回归。
// 这些都是文件路径 / 包名 / 分支名 / 词拼接，不是密钥，不应命中。
func TestEntropyFalsePositivesFromUserLog(t *testing.T) {
	falsePositives := []string{
		"/e/workspace/idea/kotlin_demo",
		"/client/exchange/MultiPlatformExchangeApi",
		"/kotlin/lingxing/model/req/ad/AdReqs",
		"/lingxing/model/req/finance/FinanceQueryReqs",
		"/kotlin/lingxing/model/resp/fba/FbaResps",
		"/resources/probe-fixtures/mp_tiktok_bills",
		"/client/exchange/WarehouseBatchExchangeTest",
		"-o-feature-lx-sdk-sync-doc-baseline-20260917",
		"/notes/061-lx-sdk-apidoc-20260917-field-diff",
		"/lx-sdk-20260917-field-diff/review-BasicData",
		"/lx-sdk-20260917-field-diff/review-VC",
		"/review-MultiPlatform",
		"/lingxing/pipeline/MultiPlatformIngestor",
		"/lingxing/pipeline/projection/AdMpProjection",
		"multiplatformexchangeapi",
		"multiplatformexchangetest",
		"/lingxing-apidoc/diffs/20260811-1120_to_20260917-2159/api-diff",
	}

	matchers := DefaultMatchers()
	for _, fp := range falsePositives {
		hits := Run(fp, matchers)
		for _, hit := range hits {
			if hit.Category == appmodel.PrivacyCategoryEntropy {
				t.Errorf("false positive %q hit entropy detector", fp)
			}
		}
	}
}

// 真密钥在收紧后仍应命中（entropy 类别显式开启时）。
func TestEntropyStillCatchesRealSecrets(t *testing.T) {
	secrets := []string{
		"aB3xK9mQ2pL7vN4wE8jR5tY1uI6oP0zH7sD4fG",   // 混合随机
		"Xk9Pm2Qw8Rt5Yu1Io6Pz0Hs7Df4Gj3LcVa6Nb",    // 混合随机变体
		"c2VjcmV0S2V5QWJjRGVmR2hpSmtMbU5wUXJzVH VXe", // base64 形态（无空格版在下面）
	}
	// base64 形态去掉空格
	secrets[2] = "c2VjcmV0S2V5QWJjRGVmR2hpSmtMbU5wUXJzVHVW"

	matchers := DefaultMatchers()
	caught := 0
	for _, s := range secrets {
		hits := Run(s, matchers)
		for _, hit := range hits {
			if hit.Category == appmodel.PrivacyCategoryEntropy {
				caught++
				break
			}
		}
	}
	if caught == 0 {
		t.Fatal("entropy detector no longer catches any high-entropy secret-like strings; tightening is too aggressive")
	}
}
