package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/modelmapping"
	"github.com/lingyuins/octopus/internal/op/setting"

	tm "github.com/lingyuins/octopus/internal/transformer/model"
)

func cacheHeaderFingerprint(headers http.Header) string {
	filtered := make(map[string][]string)
	for name, values := range headers {
		lower := strings.ToLower(name)
		if hopByHopHeaders[lower] {
			continue
		}
		switch lower {
		case "content-type", "accept", "last-event-id", "x-conversation-id":
			continue
		}
		filtered[lower] = append([]string(nil), values...)
	}
	data, _ := json.Marshal(filtered)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func cacheRequestFingerprint(req *tm.InternalLLMRequest, includeText bool) (string, bool) {
	if req == nil || len(req.Tools) > 0 {
		return "", false
	}
	for _, message := range req.Messages {
		if len(message.ToolCalls) > 0 || message.ToolCallID != nil {
			return "", false
		}
		for _, part := range message.Content.MultipleContent {
			if part.Type != "text" {
				return "", false
			}
		}
	}
	copy := *req
	copy.RawRequest = nil
	copy.Query = nil
	copy.TransformerMetadata = nil
	copy.Stream = nil
	copy.ConversationID = ""
	copy.ResumeFromEventID = 0
	copy.RawPath = ""
	copy.Query = cloneQueryValues(req.Query)
	if !includeText {
		copy.Messages = nil
	}
	var fields map[string]json.RawMessage
	if len(req.RawRequest) > 0 {
		if err := json.Unmarshal(req.RawRequest, &fields); err != nil {
			return "", false
		}
		if _, exists := fields["previous_response_id"]; exists {
			return "", false
		}
		if _, exists := fields["conversation"]; exists {
			return "", false
		}
		for _, name := range []string{"model", "messages", "input", "instructions", "stream", "stream_options", "conversation_id", "last_event_id", "resume_from_sequence"} {
			delete(fields, name)
		}
		// Unknown raw parameters remain in the exact isolation fingerprint.
	}
	data, err := json.Marshal(struct {
		Request    *tm.InternalLLMRequest
		Query      url.Values
		Parameters map[string]json.RawMessage
	}{&copy, copy.Query, fields})
	if err != nil {
		return "", false
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), true
}

func cloneQueryValues(values url.Values) url.Values {
	if len(values) == 0 {
		return nil
	}
	cloned := make(url.Values, len(values))
	for name, items := range values {
		cloned[name] = append([]string(nil), items...)
	}
	return cloned
}

func semanticNamespace(base string, req *tm.InternalLLMRequest) (string, bool) {
	fingerprint, ok := cacheRequestFingerprint(req, false)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(base) + ":v2:" + fingerprint + fmt.Sprintf(":g%d:s%d:m%d", group.Generation(), setting.Generation(), modelmapping.Generation()), true
}
