package relay

import (
	"bufio"
	"io"
	"strings"
	"sync"
)

// builderPool 复用 strings.Builder 减少堆分配
var builderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// getBuilder 从池中获取 strings.Builder
func getBuilder() *strings.Builder {
	return builderPool.Get().(*strings.Builder)
}

// putBuilder 归还 strings.Builder 到池中
func putBuilder(b *strings.Builder) {
	b.Reset()
	builderPool.Put(b)
}

// readerPool 复用 bufio.Reader 减少堆分配
var readerPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewReader(nil)
	},
}

// getReader 从池中获取 bufio.Reader，重置到新的 io.Reader
func getReader(r io.Reader) *bufio.Reader {
	br := readerPool.Get().(*bufio.Reader)
	br.Reset(r)
	return br
}

// putReader 归还 bufio.Reader 到池中
func putReader(br *bufio.Reader) {
	br.Reset(nil) // 清理引用
	readerPool.Put(br)
}
