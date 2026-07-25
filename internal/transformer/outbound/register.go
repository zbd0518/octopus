package outbound

import (
	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound/anthropic"
	"github.com/lingyuins/octopus/internal/transformer/outbound/cloudflare"
	"github.com/lingyuins/octopus/internal/transformer/outbound/codex"
	"github.com/lingyuins/octopus/internal/transformer/outbound/gemini"
	"github.com/lingyuins/octopus/internal/transformer/outbound/mimo"
	"github.com/lingyuins/octopus/internal/transformer/outbound/openai"
	"github.com/lingyuins/octopus/internal/transformer/outbound/passthrough"
	"github.com/lingyuins/octopus/internal/transformer/outbound/volcengine"
)

type OutboundType int

const (
	OutboundTypeOpenAIChat OutboundType = iota
	OutboundTypeOpenAIResponse
	OutboundTypeAnthropic
	OutboundTypeGemini
	OutboundTypeVolcengine
	OutboundTypeOpenAIEmbedding
	OutboundTypeMimo
	OutboundTypeCloudflare
	OutboundTypePassthrough
	OutboundTypeCodex
	// OutboundTypeRaw 原始穿透（信息体）：保留客户端原始请求体与请求路径，
	// 仅改写 model 字段。仅由分组出站格式 "raw" 触发，不是渠道类型。
	OutboundTypeRaw
)

func (t OutboundType) String() string {
	switch t {
	case OutboundTypeOpenAIChat:
		return "chat"
	case OutboundTypeOpenAIResponse:
		return "response"
	case OutboundTypeAnthropic:
		return "anthropic"
	case OutboundTypeGemini:
		return "gemini"
	case OutboundTypeVolcengine:
		return "volcengine"
	case OutboundTypeOpenAIEmbedding:
		return "embedding"
	case OutboundTypeMimo:
		return "mimo"
	case OutboundTypeCloudflare:
		return "cloudflare"
	case OutboundTypePassthrough:
		return "passthrough"
	case OutboundTypeCodex:
		return "codex"
	case OutboundTypeRaw:
		return "raw"
	default:
		return "unknown"
	}
}

// EmbeddingChannelTypes 定义支持 embedding 请求的 channel 类型集合
var EmbeddingChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIEmbedding: true,
}

// ChatChannelTypes 定义支持 chat 请求的 channel 类型集合
var ChatChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIChat:     true,
	OutboundTypeOpenAIResponse: true,
	OutboundTypeAnthropic:      true,
	OutboundTypeGemini:         true,
	OutboundTypeVolcengine:     true,
	OutboundTypeMimo:           true,
	OutboundTypeCloudflare:     true,
	OutboundTypeCodex:          true,
}

// IsEmbeddingChannelType 判断 channel 类型是否支持 embedding 请求
func IsEmbeddingChannelType(channelType OutboundType) bool {
	return EmbeddingChannelTypes[channelType]
}

// IsChatChannelType 判断 channel 类型是否支持 chat 请求
func IsChatChannelType(channelType OutboundType) bool {
	return ChatChannelTypes[channelType]
}

var outboundFactories = map[OutboundType]func() model.Outbound{
	OutboundTypeOpenAIChat:      func() model.Outbound { return &openai.ChatOutbound{} },
	OutboundTypeOpenAIResponse:  func() model.Outbound { return &openai.ResponseOutbound{} },
	OutboundTypeOpenAIEmbedding: func() model.Outbound { return &openai.EmbeddingOutbound{} },
	OutboundTypeAnthropic:       func() model.Outbound { return &anthropic.MessageOutbound{} },
	OutboundTypeGemini:          func() model.Outbound { return &gemini.MessagesOutbound{} },
	OutboundTypeVolcengine:      func() model.Outbound { return &volcengine.ResponseOutbound{} },
	OutboundTypeMimo:            func() model.Outbound { return &mimo.ChatOutbound{} },
	OutboundTypeCloudflare:      func() model.Outbound { return &cloudflare.ChatOutbound{} },
	OutboundTypePassthrough:     func() model.Outbound { return &passthrough.Outbound{} },
	OutboundTypeCodex:           func() model.Outbound { return &codex.Outbound{} },
	OutboundTypeRaw:             func() model.Outbound { return &passthrough.Outbound{PreservePath: true} },
}

func Get(outboundType OutboundType) model.Outbound {
	if factory, ok := outboundFactories[outboundType]; ok {
		return factory()
	}
	return nil
}
