package transformer

import (
	"fmt"
	"io"
	"net/http"
)

// MaxResponseBodyBytes 是非流式上游响应体的读取上限（50MB）。
//
// 之前 outbound transformer 的成功路径直接 io.ReadAll(response.Body)，没有任何
// 上限：单个畸形或异常巨大的上游响应（例如批量 embedding）就能让进程分配任意大
// 的 buffer。高并发下这些瞬时尖峰会互相叠加，和流会话缓冲一起把内存推向 OOM。
//
// 50MB 对正常响应来说绰绰有余——即使是大批量 embedding 响应通常也在个位数 MB
// 量级；同时它把单次读取的最坏内存占用限制在一个可控的常数上。错误路径本来就
// 已经用 io.LimitReader 做了限制（见 relay 层的 maxErrorBodyBytes）。
const MaxResponseBodyBytes = 50 << 20

// ReadResponseBody 读取上游响应体，并把读取量限制在 MaxResponseBodyBytes 内。
// 超限时返回错误而不是静默截断——截断的 JSON 反序列化后会得到语义错误的结果，
// 明确失败比返回错误数据更安全。
//
// 多读 1 字节用于区分「刚好等于上限」和「超过上限」。
func ReadResponseBody(response *http.Response) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("response body is nil")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > MaxResponseBodyBytes {
		return nil, fmt.Errorf("upstream response body too large: exceeds %d bytes", MaxResponseBodyBytes)
	}
	return body, nil
}
