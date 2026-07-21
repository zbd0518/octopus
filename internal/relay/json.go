package relay

import (
	"encoding/json"

	jsoniter "github.com/json-iterator/go"
)

// json is the JSON codec used throughout the relay layer.
// When built with -tags=jsoniter (release builds), this uses jsoniter for better performance.
// Otherwise it falls back to encoding/json for compatibility.
var jsonAPI = jsoniter.ConfigCompatibleWithStandardLibrary

// Re-export commonly used types from encoding/json for compatibility
type RawMessage = json.RawMessage
