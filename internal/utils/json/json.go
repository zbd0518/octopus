package json

import (
	"encoding/json"

	jsoniter "github.com/json-iterator/go"
)

// API is the JSON codec used throughout Octopus.
// When built with -tags=jsoniter (release builds), this uses jsoniter for better performance.
// Otherwise it falls back to encoding/json for compatibility.
var API = jsoniter.ConfigCompatibleWithStandardLibrary

// Re-export commonly used types from encoding/json for compatibility
type RawMessage = json.RawMessage

// Marshal wraps API.Marshal for convenience
func Marshal(v interface{}) ([]byte, error) {
	return API.Marshal(v)
}

// Unmarshal wraps API.Unmarshal for convenience
func Unmarshal(data []byte, v interface{}) error {
	return API.Unmarshal(data, v)
}

// Valid reports whether data is a valid JSON encoding.
func Valid(data []byte) bool {
	return API.Valid(data)
}

// MarshalIndent wraps API.MarshalIndent for convenience
func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return API.MarshalIndent(v, prefix, indent)
}

// NewDecoder creates a new JSON decoder
func NewDecoder(r interface{ Read([]byte) (int, error) }) *jsoniter.Decoder {
	return API.NewDecoder(r)
}

// NewEncoder creates a new JSON encoder
func NewEncoder(w interface{ Write([]byte) (int, error) }) *jsoniter.Encoder {
	return API.NewEncoder(w)
}
