package nodetransport

import (
	"net/http"
	"strings"
	"testing"
)

func TestOfflineStatusRetainsBoundedNonSensitiveDiagnostics(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	online := f.manager.Status(1)
	if !online.Online || online.LastSeenAt == nil {
		t.Fatal("missing online state")
	}
	f.manager.Disconnect(1)
	offline := f.manager.Status(1)
	if offline.Online || offline.LastSeenAt == nil || offline.InFlight != 0 || offline.LastError != "node disconnected" {
		t.Fatalf("missing offline diagnostic: %+v", offline)
	}
	if strings.Contains(offline.LastError, "key") || strings.Contains(offline.LastError, "http") {
		t.Fatal("dynamic secret-bearing diagnostic")
	}
	m := NewManager()
	defer m.Close()
	s := &serverSession{wire: &wire{}, limit: 4}
	s.lastSeen.Store(1)
	for id := 1; id <= 1100; id++ {
		m.rememberOffline(id, s, "node connection closed")
	}
	if len(m.history) > 1024 {
		t.Fatal("unbounded offline node history")
	}
}
