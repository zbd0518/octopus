# Attention

本文件是 CodeStable 技能启动必读的项目注意事项入口。所有 CodeStable 子技能开始工作前必须读取它。

## 报告语言

CodeStable 所有落盘产出的正文用**中文**：plan / design、plan review / design-review、code review、QA、验收、issue（report / analysis / fix-note）、refactor、roadmap、goal、沉淀（compound）等所有人读报告都用中文表达。机器状态（YAML / JSON / `state.yaml` / frontmatter 字段）保持机读格式不翻译。如需改默认语言，改这一节。

## 项目碎片知识

<!-- cs-note managed: 用 cs-note 维护，新条目按下面分节追加 -->

### 编译与构建

- Go 1.24+，模块名 `github.com/lingyuins/octopus`；前端 Next.js 16（web/），pnpm 10 管理。
- 前端产物必须放 `static/out/` 才能被 Go 二进制 `go:embed`。`static/out/*` 已被 `.gitignore` 忽略（只留 README.md），构建产物不进 git。
- CI（`.github/workflows/quality.yml`）先 `mkdir -p static/out && touch static/out/.keep` 再跑 `go test ./...`——后端测试需要该目录存在。
- `scripts/build.sh`：完整构建发布流程（前端 build → `web/out` 移到 `static/out` → 交叉编译多平台 → `build/bin|docker|archives`）。构建元数据通过 LDFLAGS 注入 `internal/conf`（Version/BuildTime/Author/Commit）。
- 价格预设表在 `internal/price/presets.go`，由 `scripts/updatePrice.py` 从 `https://models.dev/api.json` 拉取生成（编译期嵌入），网络受限需走代理。

### 运行与本地起服务

- 启动：`go run main.go start`（默认端口 8080，数据在 `data/`，SQLite `data/data.db`）。子命令还有 `version`。
- 纯 API 模式：不构建前端也能启动，但管理界面需要 `static/out/` 内嵌资源。
- 前端开发模式：`cd web && pnpm dev`（默认 3000），需设 `NEXT_PUBLIC_API_BASE_URL=http://127.0.0.1:8080` 指到后端。
- 首次启动自动生成 `data/config.json`；推荐预置 `OCTOPUS_AUTH_JWT_SECRET`（持久 JWT）和 `OCTOPUS_INITIAL_ADMIN_USERNAME/PASSWORD`（自动建 admin，密码 ≥12 字符）。
- 未配 `OCTOPUS_AUTH_JWT_SECRET` 时，JWT 密钥每次启动随机生成，重启后登录失效。

### 测试

- 根 `pnpm test` = `test:go`（`go test ./...`）+ `test:web`（i18n + 30+ 个单测文件）。
- 根 `pnpm lint` = lint:go（空测试运行=编译检查）+ lint:web（eslint）。
- `pnpm check` 全量：lint + 全部测试 + 前端生产构建。
- Go lint 严格模式：`pnpm lint:go:strict` = golangci-lint v2.6.2（`.golangci.yml`：default standard + gofmt formatter）。
- CI 里前端是 `pnpm lint` + `test:i18n` + `test:unit` + `pnpm build`（`NEXT_PUBLIC_APP_VERSION=ci`）。
- commit 时 lint-staged 会对 `*.go` 跑 `gofmt -w`、对前端文件跑 eslint（有 husky pre-commit）。

### 命令与脚本陷阱

- 当前 git 分支 `fix/site-model-case-pk-conflict`，是功能分支（有未推送 commit）。
- commit 信息用 conventional commits（`.commitlintrc.json`），scope 如 `feat(model)`、`fix(relay)`、`docs(codestable)`。
- 不要把 `web/src/components/modules/site/CheckinPanel.tsx` 的未提交改动混进 codestable 提交（那个改动与本文档无关）。
- Windows 上 `go run main.go start` 或 `scripts/build.sh` 若端口 8080 被占，先查占用再启动。
- 移动/重命名目录若遇到 "Permission denied"（如之前 `.codestable` → `codestable`），用 `powershell Rename-Item` 或 git 操作代替；git 跟踪的目录用 `git mv` / `git rm` 更稳。

### 路径与目录约定

- 对外中继 API：`POST /v1/chat/completions`、`/v1/responses`、`/v1/messages`（Anthropic）、`/v1/embeddings`，全部 API Key 鉴权（`Authorization: Bearer sk-octopus-...`，前缀 `sk-`+APP_NAME+`-`）。
- 管理 API：`/api/v1/*`（约 40 个资源组），JWT 鉴权 + RBAC（admin/editor/viewer，服务端判权）。
- 后端代码 `internal/`（server/relay/transformer/model/store/task/sitesync/planprovider/pool 等）；前端页面 `web/src/components/modules/`，路由注册在 `web/src/route/config.tsx`（13 个一级路由）。
- 权限模型：`admin`（含用户管理）/ `editor`（写运维）/ `viewer`（只读）。
- `internal/price/presets.go` 为编译期价格。
- 敏感数据存储加密：`security.encryption_key`（未配回退 JWT 密钥）。

### 环境变量与凭证

- 配置覆盖：`OCTOPUS_` + 配置路径下划线连接（`OCTOPUS_SERVER_PORT`、`OCTOPUS_DATABASE_TYPE`、`OCTOPUS_DATABASE_PATH`、`OCTOPUS_SERVER_TRUSTED_PROXIES`、`OCTOPUS_LOG_LEVEL`、`OCTOPUS_DATA_DIR` 等）。
- `OCTOPUS_AUTH_JWT_SECRET` / `auth.jwt_secret`：JWT 签名密钥（强烈建议设置，否则重启 token 失效）。
- `OCTOPUS_INITIAL_ADMIN_USERNAME/PASSWORD`：首次启动自动创建管理员。
- `OCTOPUS_SERVER_TRUSTED_PROXIES`：反代后解析真实 IP 的 CIDR（默认不信任任何代理，安全默认）。
- `NEXT_PUBLIC_API_BASE_URL`：前端开发时指向后端地址。
- `NEXT_PUBLIC_APP_VERSION`：构建时注入版本号（`git describe --tags`）。

### 其他

- 支持 SQLite / MySQL / PostgreSQL 三库实时互迁；Redis 可选（延迟/统计/熔断状态）。
- 告警可送达 8 种渠道：Webhook、Gotify、Email、Telegram、飞书、钉钉、企业微信、ntfy。
- Hub / 站点管理支持上游中继平台（New-API、One-API、One-Hub、Sub2API 等），Plan 套餐供应商（Codex、MiMo、StepFun、SenseNova、DeepSeek、Kimi、OpenRouter）。
- 语义缓存基于 embedding 相似度，非流式和流式（SSE 重放）都支持。
- WebDAV 云备份 + 定时报表 + CLI 导出（Claude Code / Codex / Gemini CLI / Cherry Studio / Kilo Code）。