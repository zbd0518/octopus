package axonhub

import (
	"fmt"
	"testing"
)

func resetGraphqlIDMapForTest(t *testing.T) {
	t.Helper()
	gqlIDMu.Lock()
	numToGraphql = make(map[int]string)
	graphqlToNum = make(map[string]int)
	nextNumericID = 1000000
	gqlIDMu.Unlock()
	t.Cleanup(func() {
		gqlIDMu.Lock()
		numToGraphql = make(map[int]string)
		graphqlToNum = make(map[string]int)
		nextNumericID = 1000000
		gqlIDMu.Unlock()
	})
}

func TestMapGraphqlID_ReusesExisting(t *testing.T) {
	resetGraphqlIDMapForTest(t)
	a := mapGraphqlID("gid://channel/1")
	b := mapGraphqlID("gid://channel/1")
	if a != b {
		t.Fatalf("reuse failed: %d vs %d", a, b)
	}
	if got, ok := resolveGraphqlID(a); !ok || got != "gid://channel/1" {
		t.Fatalf("resolve = (%q, %v)", got, ok)
	}
}

func TestMapGraphqlID_BoundedGrowth(t *testing.T) {
	resetGraphqlIDMapForTest(t)
	// 填满上限后继续插入应触发整表清空，map 不会无限增长。
	for i := 0; i < graphqlIDMapMaxEntries+50; i++ {
		mapGraphqlID(fmt.Sprintf("gid://channel/%d", i))
	}
	gqlIDMu.RLock()
	n := len(graphqlToNum)
	gqlIDMu.RUnlock()
	if n > graphqlIDMapMaxEntries {
		t.Fatalf("graphqlToNum size = %d, want <= %d", n, graphqlIDMapMaxEntries)
	}
	// 清空后新映射仍可用。
	id := mapGraphqlID("gid://channel/fresh")
	if got, ok := resolveGraphqlID(id); !ok || got != "gid://channel/fresh" {
		t.Fatalf("post-eviction resolve = (%q, %v)", got, ok)
	}
}
