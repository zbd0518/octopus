package transformer

import (
	"encoding/json"

	jsoniter "github.com/json-iterator/go"
)

// JsonAPI is the JSON codec used throughout the transformer layer.
// When built with -tags=jsoniter (release builds), this uses jsoniter for better performance.
// Otherwise it falls back to encoding/json for compatibility.
var JsonAPI = jsoniter.ConfigCompatibleWithStandardLibrary

// Re-export commonly used types from encoding/json for compatibility
type RawMessage = json.RawMessage

// Marshal wraps JsonAPI.Marshal for convenience
func Marshal(v interface{}) ([]byte, error) {
	return JsonAPI.Marshal(v)
}

// Unmarshal wraps JsonAPI.Unmarshal for convenience
func Unmarshal(data []byte, v interface{}) error {
	return JsonAPI.Unmarshal(data, v)
}

// Valid reports whether data is a valid JSON encoding.
func Valid(data []byte) bool {
	return JsonAPI.Valid(data)
}

// MarshalIndent wraps JsonAPI.MarshalIndent for convenience
func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return JsonAPI.MarshalIndent(v, prefix, indent)
}
