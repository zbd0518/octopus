package relay

import (
	"strings"
	"testing"

	tmodel "github.com/lingyuins/octopus/internal/transformer/model"
)

// deltaChunk 构造只带 delta 文本的内部 chunk
func deltaChunk(text string) *tmodel.InternalLLMResponse {
	return &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Delta: &tmodel.Message{Role: "assistant", Content: tmodel.MessageContent{Content: &text}},
		}},
	}
}

func deltaText(stream *tmodel.InternalLLMResponse) string {
	return *stream.Choices[0].Delta.Content.Content
}

func TestPrivacyStreamRestorer_Nil(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	var r *privacyStreamRestorer = pm.newStreamRestorer() // pm 为空 → nil
	if r != nil {
		t.Fatal("empty map should yield nil restorer (zero overhead)")
	}
	// nil 接收者调用应安全
	r.restoreChunk(deltaChunk("hello ⟪PII-1⟫ world"))
	r.restoreChunk(nil)
	if r.HasPending() {
		t.Fatal("nil restorer should report no pending")
	}
}

func TestPrivacyStreamRestorer_SingleChunk(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	pm.mask("13812345678")
	r := pm.newStreamRestorer()

	chunk := deltaChunk("call ⟪PII-1⟫ now")
	r.restoreChunk(chunk)
	if got := deltaText(chunk); got != "call 13812345678 now" {
		t.Fatalf("expected restored text, got %q", got)
	}
	if r.HasPending() {
		t.Fatal("complete placeholder should leave no pending carry")
	}
}

func TestPrivacyStreamRestorer_SplitAcrossChunks(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	pm.mask("13812345678")
	r := pm.newStreamRestorer()

	first := deltaChunk("call ⟪PI")
	r.restoreChunk(first)
	if got := deltaText(first); strings.Contains(got, "⟪") {
		t.Fatalf("partial placeholder must not leak, got %q", got)
	}
	if !r.HasPending() {
		t.Fatal("partial placeholder should be carried over")
	}
	if got := deltaText(first); got != "call " {
		t.Fatalf("expected only safe prefix released, got %q", got)
	}

	second := deltaChunk("I-1⟫ now")
	r.restoreChunk(second)
	if got := deltaText(second); got != "13812345678 now" {
		t.Fatalf("expected carry to complete the placeholder, got %q", got)
	}
	if r.HasPending() {
		t.Fatal("carry should be consumed")
	}
}

func TestPrivacyStreamRestorer_ChunkBoundaryAtEveryOffset(t *testing.T) {
	original := "13812345678"
	pm := newPrivacyPlaceholderMap()
	ph := pm.mask(original)

	// 在占位符的每个可能切分点切开，都应正确还原
	for split := 1; split < len(ph); split++ {
		r := pm.newStreamRestorer()
		first := deltaChunk(ph[:split])
		second := deltaChunk(ph[split:])
		r.restoreChunk(first)
		r.restoreChunk(second)
		got := deltaText(first) + deltaText(second)
		if got != original {
			t.Fatalf("split at %d: expected %q, got %q", split, original, got)
		}
	}
}

func TestPrivacyStreamRestorer_PlainTextPassesThrough(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	pm.mask("secret")
	r := pm.newStreamRestorer()

	chunk := deltaChunk("plain text without placeholder")
	r.restoreChunk(chunk)
	if got := deltaText(chunk); got != "plain text without placeholder" {
		t.Fatalf("plain text should pass through, got %q", got)
	}
	if r.HasPending() {
		t.Fatal("plain text should leave no carry")
	}
}

func TestPrivacyStreamRestorer_MultiplePlaceholdersOneChunk(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	ph1 := pm.mask("13812345678")
	ph2 := pm.mask("alice@example.com")
	r := pm.newStreamRestorer()

	chunk := deltaChunk("a " + ph1 + " b " + ph2 + " c")
	r.restoreChunk(chunk)
	want := "a 13812345678 b alice@example.com c"
	if got := deltaText(chunk); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrivacyStreamRestorer_BogusPrefixNotStuck(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	pm.mask("x")
	r := pm.newStreamRestorer()

	// ⟪PII- 后跟非数字：不能永久扣留，应原样放行
	chunk := deltaChunk("weird ⟪PII-X token")
	r.restoreChunk(chunk)
	got := deltaText(chunk)
	if !strings.Contains(got, "⟪PII-X") {
		t.Fatalf("bogus placeholder should pass through, got %q", got)
	}
	if r.HasPending() {
		t.Fatal("bogus prefix should not be carried")
	}
}

func TestPrivacyStreamRestorer_LoneBraceReleased(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	pm.mask("x")
	r := pm.newStreamRestorer()

	// 孤立 ⟪（非占位符前缀）：放行，不无限扣留
	chunk := deltaChunk("数学符号 ⟪ 在这里")
	r.restoreChunk(chunk)
	if got := deltaText(chunk); !strings.Contains(got, "⟪") {
		t.Fatalf("lone ⟪ should pass through, got %q", got)
	}
	if r.HasPending() {
		t.Fatal("lone ⟪ should not be carried")
	}
}

func TestPrivacyStreamRestorer_ToolCallArgumentsCompleteOnly(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	ph := pm.mask("13812345678")
	r := pm.newStreamRestorer()

	args := `{"phone":"` + ph + `"}`
	chunk := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Delta: &tmodel.Message{
				ToolCalls: []tmodel.ToolCall{{Function: tmodel.FunctionCall{Name: "lookup", Arguments: args}}},
			},
		}},
	}
	r.restoreChunk(chunk)
	got := chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments
	if got != `{"phone":"13812345678"}` {
		t.Fatalf("tool arguments not restored: %s", got)
	}
}

func TestPrivacyStreamRestorer_ReasoningRestored(t *testing.T) {
	pm := newPrivacyPlaceholderMap()
	ph := pm.mask("sk-abcdefghij1234567890")
	r := pm.newStreamRestorer()

	reasoning := "thinking about " + ph
	chunk := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{Delta: &tmodel.Message{ReasoningContent: &reasoning}}},
	}
	r.restoreChunk(chunk)
	if got := *chunk.Choices[0].Delta.ReasoningContent; got != "thinking about sk-abcdefghij1234567890" {
		t.Fatalf("reasoning not restored: %q", got)
	}
}
