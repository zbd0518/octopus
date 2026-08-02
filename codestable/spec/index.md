# Project Spec — Octopus

> **读者：** 第一次进入项目的开发者——「现在是什么、能做什么、从哪开始、边界在哪」。
> **状态：** 从 README、代码结构与运行经验沉淀的当前稳定事实（2026-08）。
> **主叙事：** 使用路径，不是代码目录。目标全景见 `../vision/index.md`。

---

## 这个项目是什么

**Octopus 是为个人/小团队打造的简单、美观、优雅的 LLM API 聚合与负载均衡服务**（Go 后端 + Next.js Web 管理面板，单二进制可嵌入前端）。

它解决的核心问题：**一个人拥有多个 LLM 供应商（OpenAI、Anthropic、Gemini、Claude、各地中转平台等）的多个 Key，需要一个统一入口来聚合、分流、治理、观测这些 API**。

定位差异点：
- 面向**个人与轻量运维**，不是企业级多租户平台
- 一个 Go 二进制内嵌完整 Web 管理界面，单机部署即可（Docker / 单文件）
- 强调**多用、好看、省心**：智能路由、自动签到、套餐监控、语义缓存等你平时用不上的能力也内置

## 现在能做什么、从哪进

对外只暴露**一个兼容 OpenAI / Anthropic 协议的中继入口**，管理走**内嵌 Web 面板**：

| 使用场景 | 入口 → 经过什么 → 限制看哪里 |
|---|---|
| **应用接 LLM** | 拿到 `http(s)://<host>:8080/v1` + 一个 API Key → 用 OpenAI SDK 直接调用（`/v1/chat/completions`、`/v1/responses`、`/v1/messages`、`/v1/embeddings`）→ 中继按分组做渠道选择/重试/协议互转 |
| **管理员配置渠道** | Web 面板 → 渠道 Channel 页：加多个供应商、每渠道多 Key、多端点 → 自动测延迟/测速 |
| **分流治理** | Web 面板 → 分组 Group 页：轮询/随机/故障转移/加权/Auto 智能策略；AI 路由页生成整张路由表、条件分组 |
| **模型治理** | Web 面板 → 模型 Model 页（模型广场）：价格、渠道覆盖、Key 数、延迟成功率；模型映射改写规则 |
| **Key 治理** | Web 面板 → API Key 页：白名单模型、过期时间、费用上限、RPM/TPM、按模型配额、IP 白名单 |
| **套餐监控/自动签到** | Web 面板 → 远端站点 Hub 页：管理上游中继平台账号、套餐额度监控（Codex/MiMo/StepFun/SenseNova 等）、自动签到 |
| **观测与告警** | Web 面板 → Analytics / 日志 / 通知中心：用量报表、缓存评估、错误告警（8 种通知渠道） |

**对外中继三大接口**（全部走 API Key 鉴权、JSON 体限制）：
- `POST /v1/chat/completions` — OpenAI Chat 协议
- `POST /v1/responses` — OpenAI Responses 协议
- `POST /v1/messages` — Anthropic 协议
- `POST /v1/embeddings` — OpenAI Embeddings

**管理面 API**：`/api/v1/*` 约 40 个资源组，覆盖渠道、分组、模型、APIKey、告警、日志、站点、代理池、设置、用户、审计、报表、WebDAV 备份、CLI 导出等。

## 界面、架构与语言

### 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.24 + Gin；Cobra CLI；Viper 配置；zap 日志 |
| 数据库 | SQLite（默认）/ MySQL / PostgreSQL，三者可实时迁移；Redis 可选（运行时状态、延迟、统计） |
| 存储方案 | `internal/store` 抽象，SQLite 用 `glebarez/sqlite`，Redis 用 `go-redis` |
| 中继协议 | sagernet/sing 系列（vmess/shadowsocks 代理）、tiktoken-go（token 统计）、go-sse（流式） |
| 前端 | Next.js（SSG 导出嵌入 Go）+ React + shadcn/ui + lucide + swr 类（懒加载路由） |
| 认证 | JWT（管理面）+ WebAuthn/Passkey 无密码登录 + API Key（中继面） |
| 任务 | 内置定时任务（告警、渠道过期、套餐同步、token 刷新、健康检查） |

### 后端目录结构（`internal/`）

| 包 | 职责 |
|---|---|
| `server/` | HTTP 层：server 装配、router 注册、handlers（按资源分文件）、middleware（RBAC/审计/限流/CORS/静态资源）、auth（JWT+权限）、resp |
| `relay/` | 中继核心：入站协议解析 → 渠道选择（balancer 5 策略）→ 出站转发 → 重试/熔断 → 日志/metrics；媒体中继、语义缓存 |
| `transformer/` | 协议互转（OpenAI Chat / Responses / Anthropic / Embeddings 入站出站格式转换） |
| `model/` | 领域实体与存储层：Channel、ChannelGroup、APIKey、Model、Alert、Site、Pool、ModelMapping、ErrorLog、Analytics、UsageHistory、AuditLog 等 |
| `store/` | 数据库抽象（memory/sqlite/mysql/postgres/redis），提供统一 Repository 接口 |
| `task/` | 定时任务：告警扫描、渠道过期、失败追踪 |
| `sitesync/` | 上游中继平台同步（New-API/One-API/One-Hub/Sub2API 等协议适配） |
| `planprovider/` | 套餐额度查询：Codex/MiMo/StepFun/SenseNova/DeepSeek/Kimi/OpenRouter 登录与额度 |
| `pool/` | 代理池：命名代理（直连/系统/代理池/继承）+ OAuth |
| `poolhealthcheck/`、`pooltokenrefresh/` | 代理池健康检查和 token 自动刷新 |
| `price/` | 模型价格预设表（编译期嵌入）与动态价格 |
| `update/` | 自更新检查 |
| `conf/`、`db/`、`helper/`、`utils/`、`apperror/` | 配置装载、数据库初始化、通用工具、错误定义 |
| `hub/` | 站点中心（聚合多个远端站点） |
| `transform.cmd`、`cmd/` | CLI：`start`（启动服务）、`version` |

### 前端结构

- `web/src/components/modules/` 按功能模块组织页面组件（channel、group、model、apikey、log、alert、analytics、ops、site、pool、report、notification、setting、user、plan-provider、model-mapping、credential、home 等）
- `web/src/route/config.tsx` 用懒加载注册 13 个一级路由（home/hub/channel/pool/group/model/analytics/log/notification/ops/apikey/setting/user），导航顺序可拖拽配置持久化到服务端
- 单页应用 + 自定义路由加载、内容预加载（`route/content-loader.tsx`），暗色模式、响应式移动布局

## 边界与考量

- **做**：个人/小团队单实例部署；LLM 聚合路由、Key 治理、观测告警、自动化（签到/套餐/同步）
- **不做**（截止当前）：企业多租户、SSO/SAML、细粒度按用户计费、原生集群高可用
- **多数据库动态切换**：SQLite 起步，可在线迁移到 MySQL/PG，三者共享同一套领域模型
- **权限模型**：`admin` / `editor` / `viewer` 三角色，服务端按存储角色判权（不只信 JWT 声明），写操作走审计
- **安全默认**：不信任反向代理（需显式配 CIDR 才能解析真实 IP）；初始管理员密码 ≥12 字符；JWT 密钥未配置则启动随机生成（重启失效）；敏感数据用 `security.encryption_key` 加密
- **嵌入前端**：前端构建产物放 `static/out/`，`go:embed` 进二进制；API-only 模式可先启动后端
- **质量约束**：中继正确性（协议转换、流式、重试幂等）是首要承诺；冷启动成本低（个人场景），比高并发更优先的是可靠性

## 想深入时

- 理解产品全景与目标 → `../vision/index.md`
- 了解运行/构建细节 → `../attention.md`
- 改中继链路（渠道选择/重试/协议） → `internal/relay/` + `internal/transformer/`
- 加管理 API → `internal/server/handlers/` + `internal/server/router/router.go` 注册模式
- 改前端页面 → `web/src/components/modules/` + `web/src/route/config.tsx`
- 历史 issue 与功能记录 → `../issues/`（已对齐编号 `001-x-`…`012-x-`）