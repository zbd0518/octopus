package plugin

import (
	"context"
	"encoding/json"
	"testing"
)

// testPlugin builds a minimal Extism WASM module from raw bytes. It is a
// placeholder for full host ABI integration tests; the wazerotest-based
// module does not expose the Extism guest protocol.
func testPlugin(t *testing.T, manifest Manifest, wasm []byte) *generation {
	t.Helper()
	record := PluginRecord{ID: manifest.ID, ManifestJSON: mustJSON(t, manifest), Enabled: true}
	g, err := newGeneration(context.Background(), record, wasm)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestManifestValidation(t *testing.T) {
	m := Manifest{HostAPI: HostAPIVersion, ID: "test-plugin", Name: "Test", Version: "1.0.0"}
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if m.TimeoutMS != 100 || m.MemoryMiB != 16 || m.Instances != 4 {
		t.Fatalf("defaults not applied: %+v", m)
	}

	bad := Manifest{HostAPI: 2, ID: "x", Name: "x", Version: "1.0.0"}
	if err := bad.Validate(); err == nil {
		t.Fatal("incompatible host API accepted")
	}

	badID := Manifest{HostAPI: HostAPIVersion, ID: "Bad ID", Name: "x", Version: "1.0.0"}
	if err := badID.Validate(); err == nil {
		t.Fatal("invalid plugin id accepted")
	}
}
