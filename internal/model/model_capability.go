package model

// ModelCapability describes which endpoints a model supports,
// whether it belongs to the conversation family, and whether
// it is currently available (has at least one enabled channel).
type ModelCapability struct {
	Name          string   `json:"name"`
	CanonicalName string   `json:"canonical_name,omitempty"`
	Aliases       []string `json:"aliases,omitempty"`
	Endpoints     []string `json:"endpoints"`
	Conversation  bool     `json:"conversation"`
	Available     bool     `json:"available"`
}
