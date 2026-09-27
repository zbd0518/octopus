package pool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lingyuins/octopus/internal/client"
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/helper"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/crypto"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

// poolTestDBDir 是 TestMain 为本测试进程创建的临时库目录，退出前删除。
var poolTestDBDir string

// TestMain 在测试环境初始化加密密钥（生产由 cmd/start.go 强制初始化）与主库。
// 加密密钥沿用仓库既有测试约定的固定字符串，勿改。
//
// 主库必须「每进程只初始化一次」，这是本文件能在 -count>=2 下通过的前提：
// db.InitDB → Migrate → migrate.AfterAutoMigrate 跑完会把注册表清空
// （afterAutoMigrations = nil），而 account_pools / pool_accounts 只由迁移 040
// 创建、且不在 db.Migrate 的 AutoMigrate 模型列表里。于是第二次 InitDB 既无迁移
// 可跑、AutoMigrate 又不建这两张表 ⇒ CreatePool 以 "no such table: account_pools"
// 失败。per-test 调 InitDB 的包（如 internal/op/backup）不受影响，因为它们依赖的
// 表都在 AutoMigrate 列表内。
//
// 测试之间改用 purgePoolTestData 只清数据、不关库。
func TestMain(m *testing.M) {
	crypto.Init("octopus-test-encryption-key")

	dir, err := os.MkdirTemp("", "octopus-pool-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "MkdirTemp: %v\n", err)
		os.Exit(1)
	}
	poolTestDBDir = dir

	code := 1
	if err := db.InitDB("sqlite", filepath.Join(dir, "main.db"), false); err != nil {
		fmt.Fprintf(os.Stderr, "InitDB: %v\n", err)
	} else {
		code = m.Run()
		_ = db.Close()
	}
	_ = os.RemoveAll(poolTestDBDir)
	os.Exit(code)
}

// setupPoolTestDB 校验 TestMain 建好的主库可用，并注册「只清数据、不关库」的清理。
//
// 前后各清一次：Cleanup 覆盖正常路径，setup 处的即时清理兜住上一轮 Fatalf 之后
// 的残留（account_pools.name 有 uniqueIndex，残留行会让本轮 CreatePool 撞唯一约束）。
// 库连接全程复用，因此 db.InitDB 只跑一次，迁移 040 建的表始终在位。
func setupPoolTestDB(t *testing.T) {
	t.Helper()

	if db.GetDB() == nil {
		t.Fatal("主库未初始化：TestMain 的 db.InitDB 未成功")
	}
	purgePoolTestData(t)
	t.Cleanup(func() { purgePoolTestData(t) })
}

// purgePoolTestData 清空本包测试写入的表：先子表 pool_accounts，再父表 account_pools。
// 两表都没有数据库级外键约束（model 未声明 foreignKey/constraint tag，PoolAccount
// 也没有 gorm.DeletedAt 软删除），顺序只为可读性。渠道侧的 pool_id 由 DeletePool
// 负责解绑，本包测试不建渠道，故不在此清理。
func purgePoolTestData(t *testing.T) {
	t.Helper()

	if err := db.GetDB().Exec("DELETE FROM pool_accounts").Error; err != nil {
		t.Fatalf("purge pool_accounts: %v", err)
	}
	if err := db.GetDB().Exec("DELETE FROM account_pools").Error; err != nil {
		t.Fatalf("purge account_pools: %v", err)
	}
}

// TestTestAccountDoesNotPolluteSharedHTTPClient 是 S-0 的回归守卫。
//
// 缺陷：TestAccount 拿到 helper.PoolAccountHttpClient 返回的指针后直接写
// `client.Timeout = testTimeout`。而该指针就是 client 包按 (bucket, proxyURL)
// 缓存的共享单例（getOrBuildCustomProxyClient 全程 `return c, nil`，无拷贝），
// internal/relay/relay.go 的 sendRequest 用的是同一个指针 ⇒ 任何一次号池账号测试
// 都会给该 proxyURL 的 default 档永久装上 30s 整体超时（直到进程重启），此后所有
// 走该代理的转发流式请求会在 30 秒被砍断，静默、无日志、不自愈。
//
// 触发面不止管理端手动点击：internal/poolhealthcheck 的 probeOnce → pool.TestAccount
// 是注册在案的周期任务（internal/task/init.go），开启号池健康巡检后会自动定时污染。
//
// 未修复时本测试必然失败：预热得到的 default 档 Timeout 为 0（不限，长流式响应的
// 前提），TestAccount 之后会变成 30s。修复（浅拷贝 `cloned := *pc` 后再设超时）
// 让共享单例保持 0。
func TestTestAccountDoesNotPolluteSharedHTTPClient(t *testing.T) {
	setupPoolTestDB(t)

	// 上游走 httptest（127.0.0.1），按仓库约定临时放行私有地址。
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[]}`))
	}))
	t.Cleanup(upstream.Close)

	// 唯一化的 proxyURL：client 包的缓存是进程级全局 map，避免与其他测试串味。
	// 端口 9（discard）上没有监听，拨号会立刻 connection refused —— 请求必然失败，
	// 但污染发生在 client.Do 之前，所以不影响本测试的断言。
	const proxyURL = "http://127.0.0.1:9/proxy-config-under-test"

	origResolver := helper.ProxyURLByConfigFunc
	helper.ProxyURLByConfigFunc = func(id int, _ context.Context) (string, error) {
		return proxyURL, nil
	}
	t.Cleanup(func() { helper.ProxyURLByConfigFunc = origResolver })

	// 预热共享单例，并断言前置条件：default 档的整体超时必须为 0（不限）。
	// 这正是 relay 长流式响应（reasoning 模型 + 长输出）不被中途砍断的前提。
	shared, err := client.GetHTTPClientCustomProxy(proxyURL)
	if err != nil {
		t.Fatalf("GetHTTPClientCustomProxy: %v", err)
	}
	if shared == nil {
		t.Fatal("GetHTTPClientCustomProxy returned nil client")
	}
	if shared.Timeout != 0 {
		t.Fatalf("前置条件不成立：default 档共享 client Timeout = %v, want 0", shared.Timeout)
	}

	proxyConfigID := 7
	pool := &model.AccountPool{Name: "shared-client-pollution-guard", Enabled: true}
	if err := CreatePool(pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	if pool.ID <= 0 {
		t.Fatalf("CreatePool 未回填自增 ID：%d", pool.ID)
	}
	account := &model.PoolAccount{
		PoolID:        pool.ID,
		Name:          "guard-account",
		Platform:      model.PoolPlatformOpenAI,
		Type:          model.PoolTypeAPIKey,
		Credentials:   `{"type":"apikey","api_key":"***"}`,
		BaseURL:       upstream.URL,
		Status:        "active",
		Schedulable:   true,
		ProxyConfigID: &proxyConfigID,
	}
	if err := CreateAccount(account); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// 请求本身会因为假代理拨号失败而返回 Success=false，这是预期的：
	// 本测试只关心共享单例有没有被改写。
	if _, err := TestAccount(account.PoolID, account.ID, "gpt-4o-mini"); err != nil {
		t.Fatalf("TestAccount: %v", err)
	}

	after, err := client.GetHTTPClientCustomProxy(proxyURL)
	if err != nil {
		t.Fatalf("GetHTTPClientCustomProxy (after): %v", err)
	}
	if after != shared {
		t.Fatalf("client 缓存未命中同一单例：%p != %p", after, shared)
	}
	if after.Timeout != 0 {
		t.Errorf("共享 HTTP client 单例被号池账号测试污染：Timeout = %v, want 0\n"+
			"这会让该代理的转发流式请求在 %v 处被砍断且不自愈；"+
			"TestAccount 必须先浅拷贝（cloned := *pc）再设超时。", after.Timeout, testTimeout)
	}
}

// TestTestAccountShallowCopyKeepsTransportShared 锁住修复方式的另一半：
// 浅拷贝 http.Client 只复制四个字段（Transport/CheckRedirect/Jar/Timeout），
// Transport 指针是共享的 ⇒ 连接池照常复用，不会退化成「每次测试新建一套
// transport + 空闲连接池」（issue #124 当初引入缓存要解决的正是这个）。
// 同时 Timeout 必须在副本上独立生效，不回写单例。
func TestTestAccountShallowCopyKeepsTransportShared(t *testing.T) {
	const proxyURL = "http://127.0.0.1:9/shallow-copy-transport-guard"

	shared, err := client.GetHTTPClientCustomProxy(proxyURL)
	if err != nil {
		t.Fatalf("GetHTTPClientCustomProxy: %v", err)
	}

	// 与生产代码同形态的浅拷贝。
	cloned := *shared
	cloned.Timeout = testTimeout

	if cloned.Transport != shared.Transport {
		t.Errorf("浅拷贝后 Transport 指针不再共享：%p != %p（连接池将无法复用）", cloned.Transport, shared.Transport)
	}
	if cloned.Jar != shared.Jar {
		t.Errorf("浅拷贝后 Jar 不一致：%v != %v", cloned.Jar, shared.Jar)
	}
	if cloned.Timeout != testTimeout {
		t.Errorf("副本 Timeout = %v, want %v", cloned.Timeout, testTimeout)
	}
	if shared.Timeout != 0 {
		t.Errorf("共享单例 Timeout = %v, want 0（副本的超时不得回写到单例）", shared.Timeout)
	}
}
