package task

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/conf"
	"github.com/lingyuins/octopus/internal/price"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

// 本文件锁住一处已确证的缺陷修复：价格更新周期任务不得用裸 context.Background()
// 出站到 models.dev。
//
// 背景（为什么裸 Background 会让整个进程的价格更新永久停摆）：
//
//	price.UpdateLLMPrice 走 internal/client 的 default 档。该档的
//	http.Client.Timeout 被有意置为 0（不限），僵死保护只剩
//	Transport.ResponseHeaderTimeout——而它**只覆盖「等响应头」阶段**。
//	price.go 的响应体读取是 io.ReadAll(io.LimitReader(resp.Body, 10MiB+1))，
//	LimitReader 限制的是「大小」不是「时间」。于是上游发完响应头后以极慢速率
//	涓流 body 时，该 goroutine 会**永久挂住**。
//
//	后果是连锁的：task.go 的 runOnce 用 running.CompareAndSwap + defer
//	running.Store(false) 防重叠运行，永久挂住的 entry.fn() 永不执行 defer ⇒
//	此后每个 tick 都被 "skipping overlapping run" 跳过 ⇒ 价格更新功能永久停摆；
//	同时 Shutdown 的 entry.wg.Wait() 永久阻塞 ⇒ 优雅关闭卡死，且 task.Shutdown
//	之后的 db.StopSerialWriter / op.SaveCache 等 hook 不执行 ⇒ 有数据丢失风险。
//
// 为什么用源码文本守卫（*_mem_test.go 模式，仓库既有先例：
// internal/op/ops/ops_mem_test.go 用 runtime.Caller(0) + os.ReadFile 读源文件断言）：
//
//	Register 的任务体是注册时捕获的闭包，且 tasks 是包级全局注册表（重复注册同名
//	任务会被 "already registered, skipping" 静默跳过）。要在运行时取出「init.go 里
//	那个价格更新闭包」并断言它带超时，必须触发真实 Init()——那会连带启动
//	serial writer / flush worker / 注册十余个任务，污染全局注册表且无法复原，
//	还可能真实出站到 models.dev。因此改用源码守卫：它精确、零副作用、不受
//	注册表状态影响。行为侧的补充验证见
//	TestUpdateLLMPriceBodyReadHonorsContextDeadline。
//
// 注意：本文件的断言**只在提取出的价格任务闭包体内**做，不断言「init.go 全文不含
// context.Background()」——其他任务（errorlog.Cleanup / relaylog / remotesite /
// backup / porop 等）的 context.Background() 各有其正当性或属于独立的后续议题，
// 且 context.WithTimeout(context.Background(), ...) 本身就含 context.Background()
// 字面量，全文断言必然误伤。

// loadInitSource 读取 internal/task/init.go 源码，并归一化行尾。
//
// 归一化是必需的：仓库 core.autocrlf=true，工作区里的 .go 文件实际是 CRLF。
// 若不先把 \r\n 换成 \n，后面按 "\n\t\t})" 切闭包体会直接切不到。
func loadInitSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(file), "init.go")))
	if err != nil {
		t.Fatalf("read init.go: %v", err)
	}
	return strings.ReplaceAll(string(src), "\r\n", "\n")
}

// priceTaskBody 提取 init.go 中价格更新任务注册的那个 func() 闭包体。
//
// 以 Register(string(model.SettingKeyModelInfoUpdateInterval), ...) 为起点，
// 到其后第一个 "\n\t\t})"（即该 Register 调用的收尾）为终点。这样切出来的范围
// 只含价格任务自己，不会溢出到紧邻的 TaskBaseUrlDelay 等其他注册。
func priceTaskBody(t *testing.T) string {
	t.Helper()
	src := loadInitSource(t)

	const anchor = "Register(string(model.SettingKeyModelInfoUpdateInterval), priceUpdateInterval, true, func() {"
	idx := strings.Index(src, anchor)
	if idx < 0 {
		t.Fatalf("price task registration anchor not found in init.go: %q\n"+
			"若价格任务的注册写法被有意改动，请同步更新本守卫的 anchor。", anchor)
	}
	rest := src[idx+len(anchor):]
	end := strings.Index(rest, "\n\t\t})")
	if end < 0 {
		t.Fatal("price task closure terminator \"\\n\\t\\t})\" not found after the registration anchor")
	}
	return rest[:end]
}

// TestPriceTaskUsesTimeoutContext 是主守卫：价格更新任务体内必须创建带超时的
// ctx 并把它传给 price.UpdateLLMPrice，不得再把裸 context.Background() 直接传出去。
func TestPriceTaskUsesTimeoutContext(t *testing.T) {
	body := priceTaskBody(t)

	// 1) 闭包体内必须出现 context.WithTimeout(context.Background(), ...)。
	//    Register 的 fn 签名是 func()（无 ctx 参数），所以超时 ctx 只能在体内创建，
	//    这也正是 op/remotesite/remotesite.go 既有先例的写法。
	withTimeoutRe := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*context\.WithTimeout\(context\.Background\(\),\s*([0-9]+)\s*\*\s*time\.(Minute|Second|Hour)\)`)
	m := withTimeoutRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("price task body must create a timeout context via context.WithTimeout(context.Background(), N*time.Unit); body:\n%s", body)
	}
	ctxVar, cancelVar, numStr, unit := m[1], m[2], m[3], m[4]

	// 2) 必须 defer cancel()，否则 ctx 及其定时器泄漏（govet lostcancel 也会报）。
	if !strings.Contains(body, "defer "+cancelVar+"()") {
		t.Errorf("price task body must \"defer %s()\" to release the context; body:\n%s", cancelVar, body)
	}

	// 3) 必须把那个带超时的 ctx 传给 UpdateLLMPrice（而不是别的变量 / 不是裸 Background）。
	if !strings.Contains(body, "price.UpdateLLMPrice("+ctxVar+")") {
		t.Errorf("price task body must call price.UpdateLLMPrice(%s) with the timeout context; body:\n%s", ctxVar, body)
	}

	// 4) 裸形态必须消失。这是缺陷本身的形状，逐字断言最直白。
	if strings.Contains(body, "price.UpdateLLMPrice(context.Background())") {
		t.Errorf("price task body still passes a bare context.Background() to price.UpdateLLMPrice: "+
			"models.dev 涓流响应体会让该 goroutine 永久挂住，价格更新停摆且优雅关闭阻塞。body:\n%s", body)
	}

	// 5) 超时量级必须显著小于任务间隔。间隔单位是小时（SettingKeyModelInfoUpdateInterval，
	//    默认 24h），所以这里只接受秒/分钟量级，出现 time.Hour 即为回归。
	if unit == "Hour" {
		t.Errorf("price task timeout must be far smaller than its (hour-scale) interval, got %s*time.Hour; body:\n%s", numStr, body)
	}
	n := 0
	for _, c := range numStr {
		n = n*10 + int(c-'0')
	}
	var timeout time.Duration
	switch unit {
	case "Second":
		timeout = time.Duration(n) * time.Second
	case "Minute":
		timeout = time.Duration(n) * time.Minute
	case "Hour":
		timeout = time.Duration(n) * time.Hour
	}
	// 间隔最小实用值是 1 小时（设置项单位就是小时）；超时须 <= 10 分钟才算「显著小于」。
	if timeout > 10*time.Minute {
		t.Errorf("price task timeout %v is not comfortably below the minimum practical interval (1h)", timeout)
	}
	if timeout <= 0 {
		t.Errorf("price task timeout must be positive, got %v", timeout)
	}
	t.Logf("price task timeout = %v (%s*time.%s), interval is hour-scale (default 24h)", timeout, numStr, unit)
}

// TestUpdateLLMPriceBodyReadHonorsContextDeadline 是守卫的行为侧补充：证明
// 「带 deadline 的 ctx 确实能中断**响应体读取阶段**」——即本次修复的机制真的有效，
// 而不只是改对了字面量。
//
// 这一步值得单独验证，因为 default 档保留的 Transport.ResponseHeaderTimeout
// 只覆盖到收到响应头为止；若 ctx 对 body 读取无效，本修复就防不住涓流。
// net/http 的语义是 ctx 取消会中断 resp.Body 的后续 Read，这里用本地 server 实证。
func TestUpdateLLMPriceBodyReadHonorsContextDeadline(t *testing.T) {
	// httptest 监听 127.0.0.1，而 internal/client 的直连路径注入了
	// xurl.SafeDialContext（默认拒绝私网/环回）。按仓库既有约定打开测试豁免，
	// 并在 cleanup 关回——生产恒为 false。
	xurl.SetSSRFAllowPrivateForTest(true)
	defer xurl.SetSSRFAllowPrivateForTest(false)

	headersSent := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case headersSent <- struct{}{}:
		default:
		}
		// 响应头已发出，此后一个 body 字节都不吐 —— 模拟「无限慢速涓流」的极端形态。
		// 等客户端取消后 r.Context() 会被 done，handler 立即返回，不泄漏 goroutine。
		<-r.Context().Done()
	}))
	defer srv.Close()

	// 把 models.dev 指向本地 server。conf.AppConfig 是包级全局，测试后必须还原。
	prevURL := conf.AppConfig.External.LLMPriceURL
	conf.AppConfig.External.LLMPriceURL = srv.URL
	defer func() { conf.AppConfig.External.LLMPriceURL = prevURL }()

	const deadline = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	start := time.Now()
	err := price.UpdateLLMPrice(ctx)
	elapsed := time.Since(start)

	// 事后确认 server 确实已发出响应头（handler 跑到了 Flush）。这样 default 档
	// 保留的 Transport.ResponseHeaderTimeout（600s）不可能是中断原因，能解释
	// elapsed 量级的只剩 ctx deadline。
	select {
	case <-headersSent:
	default:
		t.Fatal("test server never produced response headers; the request did not reach the handler")
	}

	if err == nil {
		t.Fatal("UpdateLLMPrice unexpectedly succeeded against a server that never sends a body")
	}
	// 关键断言：deadline 量级内就返回，而不是永久挂住。留足调度余量避免 flaky。
	if elapsed > 5*time.Second {
		t.Fatalf("UpdateLLMPrice did not return within the ctx deadline: elapsed=%v, err=%v "+
			"（说明 body 读取阶段不受 ctx 约束，本次修复的前提不成立）", elapsed, err)
	}
	t.Logf("body read interrupted by ctx after %v: %v", elapsed, err)
}
