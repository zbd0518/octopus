package relay

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/transformer/inbound"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

func TestResponsesTruncationReachesUpstream(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		paramOverride string
		want          string
	}{
		{"client_auto", `{"model":"gpt-5.6-sol","input":"hello","truncation":"auto"}`, "", `"auto"`},
		{"client_disabled", `{"model":"gpt-5.6-sol","input":"hello","truncation":"disabled"}`, "", `"disabled"`},
		{"unspecified_defaults_to_auto", `{"model":"gpt-5.6-sol","input":"hello"}`, "", `"auto"`},
		{"null_defaults_to_auto", `{"model":"gpt-5.6-sol","input":"hello","truncation":null}`, "", `"auto"`},
		{"channel_auto", `{"model":"gpt-5.6-sol","input":"hello"}`, `{"truncation":"auto"}`, `"auto"`},
		{"channel_disabled", `{"model":"gpt-5.6-sol","input":"hello"}`, `{"truncation":"disabled"}`, `"disabled"`},
		{"client_disabled_wins", `{"model":"gpt-5.6-sol","input":"hello","truncation":"disabled"}`, `{"truncation":"auto"}`, `"disabled"`},
		{"client_auto_wins", `{"model":"gpt-5.6-sol","input":"hello","truncation":"auto"}`, `{"truncation":"disabled"}`, `"auto"`},
		{"invalid_channel_value_defaults_to_auto", `{"model":"gpt-5.6-sol","input":"hello"}`, `{"truncation":"invalid"}`, `"auto"`},
		{"invalid_channel_type_defaults_to_auto", `{"model":"gpt-5.6-sol","input":"hello"}`, `{"truncation":true}`, `"auto"`},
		{"null_channel_value_defaults_to_auto", `{"model":"gpt-5.6-sol","input":"hello"}`, `{"truncation":null}`, `"auto"`},
		{"stream_auto", `{"model":"gpt-5.6-sol","input":"hello","stream":true,"truncation":"auto"}`, "", `"auto"`},
	}
	for _, profile := range []appmodel.RequestRewriteProfile{"", appmodel.RequestRewriteProfilePreserve} {
		name := "rewrite_disabled"
		if profile != "" {
			name = "rewrite_enabled"
		}
		t.Run(name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					ctx := context.Background()
					request, err := inbound.Get(inbound.InboundTypeOpenAIResponse).TransformRequest(ctx, []byte(tt.body))
					if err != nil {
						t.Fatal(err)
					}
					channel := &appmodel.Channel{Type: outbound.OutboundTypeOpenAIResponse, ParamOverride: &tt.paramOverride}
					if profile != "" {
						channel.RequestRewrite = &appmodel.RequestRewriteConfig{Enabled: true, Profile: profile}
					}
					prepared, _, err := prepareInternalRequestForOutbound(channel, request, nil)
					if err != nil {
						t.Fatal(err)
					}
					wire, err := outbound.Get(outbound.OutboundTypeOpenAIResponse).TransformRequest(ctx, prepared, "https://example.invalid", "")
					if err != nil {
						t.Fatal(err)
					}
					defer wire.Body.Close()
					body, err := io.ReadAll(wire.Body)
					if err != nil {
						t.Fatal(err)
					}
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(body, &fields); err != nil {
						t.Fatal(err)
					}
					if got := string(fields["truncation"]); got != tt.want {
						t.Fatalf("outbound truncation = %q, want %q; body=%s", got, tt.want, body)
					}
				})
			}
		})
	}
}

func TestChannelTruncationDefaultIsScopedToAttempt(t *testing.T) {
	ctx := context.Background()
	request, err := inbound.Get(inbound.InboundTypeOpenAIResponse).TransformRequest(ctx, []byte(`{"model":"gpt-5.6-sol","input":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		override string
		want     string
	}{
		{`{"truncation":"auto"}`, `"auto"`},
		{`{"truncation":"disabled"}`, `"disabled"`},
		{"", `"auto"`},
	} {
		channel := &appmodel.Channel{Type: outbound.OutboundTypeOpenAIResponse, ParamOverride: &tt.override}
		prepared, _, err := prepareInternalRequestForOutbound(channel, request, nil)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := outbound.Get(outbound.OutboundTypeOpenAIResponse).TransformRequest(ctx, prepared, "https://example.invalid", "")
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		err = json.NewDecoder(wire.Body).Decode(&fields)
		wire.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := string(fields["truncation"]); got != tt.want {
			t.Fatalf("override %q: outbound truncation = %q, want %q", tt.override, got, tt.want)
		}
	}
}

func TestResponsesTruncationDoesNotLeakToChatOutbound(t *testing.T) {
	ctx := context.Background()
	request, err := inbound.Get(inbound.InboundTypeOpenAIResponse).TransformRequest(ctx, []byte(`{"model":"gpt-5.6-sol","input":"hello","truncation":"auto"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse} {
		wire, err := outbound.Get(kind).TransformRequest(ctx, request, "https://example.invalid", "")
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		err = json.NewDecoder(wire.Body).Decode(&fields)
		wire.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if kind == outbound.OutboundTypeOpenAIResponse {
			want = `"auto"`
		}
		if got := string(fields["truncation"]); got != want {
			t.Fatalf("%s outbound truncation = %q, want %q", kind, got, want)
		}
	}
}
