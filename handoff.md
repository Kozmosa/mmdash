# mmdash v0.1 Stage 5 Agent Sessions handoff

- Updated: 2026-08-06
- Branch: `codex/stage-5-agent-sessions`
- Base: `codex/stage-4-home-progress@202bad8`（含全部已验收 Stage 4 fixes，于 `141ad72` 整合）
- Delivery state: Stage 5 implementation complete；12 个提交，HEAD `b7118cb`；
  未 push、未创建 PR

## Status

Stage 5 is complete against the v0.1 implementation-order v0.4,
technical-architecture v0.4, and product-design v0.1 baselines. The Agent
module is the Core authority for Runtime-independent Agent instances, Project
Grants, management modes, project Prompt overrides, Session indexes, remote
Session/Run mappings, normalized Tool Call state, Token-rotation
orchestration, Audit, and Outbox events. Hermes owns full message history and
execution; Web, Web BFF, MCP Gateway, and Data Hub do not read Agent tables
directly. Only `HermesAdapter` is implemented; `AgentAdapter` remains the
provider-neutral extension boundary. Stage 6 automatic Progress tracking
(Cron, event triggers, debounce) is deliberately not implemented.

## Delivered behavior

- Runtime-independent `AgentAdapter` port（能力探测、Session 生命周期、消息、
  事件流、Run、Job、项目访问配置/验证、统一错误），注册元数据声明
  `project_access.verify/configure/rotate`；设置页按声明能力决定是否提供
  `auto`（`fix(web): gate auto management on adapter capabilities`）。
- `HermesAdapter`（pinned v2026.8.3）覆盖健康、鉴权、版本能力检查，
  Sessions CRUD/Fork/Messages、Chat/StreamChat、Runs 启停/审批/SSE、
  Jobs 全量映射，以及 Dashboard 管理 API（MCP servers 安装/更新/测试、
  Gateway reload/restart）。
- `manual / auto` 管理模式。manual 不保存管理凭证、可选 Dashboard 入口仅
  跳转；auto 支持 `direct` 与 `cloudflare_access` 管理链路探测，探测结果
  持久化，每次连接测试重新验证，未通过拒绝启用。
- 受控 Connector 出站策略：仅 http(s) 与端口白名单、禁 URL 内嵌凭证、每次
  DNS 解析与重定向逐跳重校验、拒绝 link-local/云元数据/未授权私网、
  loopback/私网需部署策略允许、超时/速率限制/脱敏日志/Audit。
- 产品级 Agent Token：高熵 opaque Token，服务端仅存 SHA-256 Hash，明文只
  返回一次；绑定 Agent 实例/Project/精确 Tool 名单；`pending -> active ->
  revoked` 两阶段轮换，失败保留旧 Token；立即撤销；MCP Gateway 校验
  Token/Project/Tool 并通过 `X-Mmdash-Gateway-Authorization` 将 Agent 身份
  传给 Core 二次领域鉴权与 Audit。生产镜像（`NODE_ENV=production`）拒绝
  静态 `MCP_AGENT_TOKEN`，`compose.yaml`/`.env.example` 默认已同步为空。
- Session 生命周期：创建/切换/重命名/结束/继续/分叉/停止/重新生成/重新
  执行；`main / progress / experiment` 类型；本地仅存索引与远端 ID 映射；
  完整消息历史由 Hermes 保存。
- 项目 Prompt 自动生成、手工修改（PATCH）、恢复默认（POST reset）。
- `context.promote` 经 MCP Gateway 接通：权限与结构校验、Agent
  Session/Run 成对 provenance、结果始终为待人工 Review 的 Context
  Proposal。
- Web 会话工作台：会话列表、消息历史、输入框、BFF SSE 流式代理、工具调用
  展示、Run 状态、停止/新建/分叉/重新生成/重新执行、连接状态与
  manual/auto 展示、manual 可选 Dashboard 入口。浏览器不持有 Hermes API
  Key 或 Agent Token。

## Acceptance fixes discovered in this run

- `AS grant` 是 PostgreSQL 保留关键字，Agent 查询中作为表别名导致运行时
  语法错误；统一改名 `grant_row`（instance/role/provenance/default-session
  查询）。
- `data_objects` 的 context-proposal 插入把同一参数同时绑定 uuid 与 text
  列；改为独立 `source_id` 参数，并新增
  `backend/internal/datahub/context_proposal_integration_test.go` 防回归。
- 生产 `mcp-gateway` 镜像拒绝非空 `MCP_AGENT_TOKEN`；compose 与 .env 默认
  改为空。
- 后端镜像保证迁移/契约文件对非 root `mmdash` 用户可读。
- Web 设置页 `auto` 选项按 adapter 声明能力禁用。

## Contracts and persistence

Migration `000026_agent_sessions` 拥有 `agent_instances`（含运行时/管理面
检查快照与能力）、`agent_project_grants`（精确 Tool 范围、默认 Session、
Prompt override、远端项目访问引用）、`agent_sessions`（main/progress/
experiment 索引与远端 ID）、`agent_runs`、`agent_tool_calls`；Auth 侧
`auth_agent_tokens` 只存 hash 与验证证据、`agent_token_rotations` 轮换状态；
`data_context_proposals` 增加 `proposed_by_actor_id/kind`（agent 行为）与
Session/Run 外键。迁移 `000023-000025` 属于已验收 Stage 4 fixes，编号连续
无冲突，fresh DB 全量迁移通过。Hermes API Key、Dashboard Session Token、
Cloudflare 凭证分别加密存于 resource-scoped Settings。Agent 生命周期事件
（instance created/updated/revoked、session created/ended/forked、run
started/completed、token rotated/revoked、context.proposal.created）走标准
Outbox，事件目录、OpenAPI、生成的 Go/TS 客户端与 API 目录已对齐。

## Verification

Passed:

- `pnpm contracts:generate`、`pnpm contracts:check`、`pnpm api:check`
  （315 operations / 8 contracts）、`pnpm caddy:check`（镜像需经本地
  mirror 拉取后通过）。
- TypeScript lint、全量 TS 测试（web 75、web-bff 38、mcp-gateway 31）、
  TS 构建；Go 格式化/lint/构建；除 repo 包 git 集成超时测试外全部 Go 测试
  通过（含 `MMDASH_TEST_DATABASE_URL` 下的 agent/auth/datahub/progress/
  project/settings integration tests）；Python lint/测试/构建。
- Docker Compose acceptance（隔离端口 13000/13001/15432/18080/19000/
  19001/18642/19002）：`scripts/mock-hermes.mjs`（pinned v2026.8.3 契约）
  + `scripts/agent-smoke.mjs` 35 项断言全绿（实例设置、运行时检查、
  tools/list 精确工具证据、pending 拒绝、VerifyToken 激活、反向验证、
  会话/消息/Run/SSE/停止/重跑/重生成/分叉/结束、context.promote、Prompt
  覆盖与恢复、两阶段轮换/中止/撤销、撤销后 Gateway 拒绝）。
- `pnpm smoke` 通过（`MMDASH_SMOKE_SKIP_CLI=1`，原因见限制）；Web/BFF/
  Core/MCP 健康检查全绿；修复后容器日志零 panic/fatal/error，且不包含
  Hermes API Key、Dashboard Token、Cloudflare Secret 或 Agent/API Token
  明文。栈以 `docker compose down`（不带 `-v`）停止，命名卷保留。

## Known limitations

- 未执行真实 Hermes interoperability：验收使用 pinned mock HTTP/SSE
  服务器（`scripts/mock-hermes.mjs` + `scripts/agent-smoke.mjs`）。真实
  Hermes 实例的互操作属独立环境检查，需在具备 Hermes 的环境中另行执行。
- `backend/internal/repo` 的 git 集成测试在无外网/慢 git 环境超时
  （Stage 4 基线同样失败，环境性 pre-existing 问题）。
- Native CLI smoke 需要系统 Secret Service keyring；headless 主机无
  keyring，故 `MMDASH_SMOKE_SKIP_CLI=1` 跳过 CLI 段（其余 smoke 照跑）。
- `StreamChat` 为已实现并测试的接口端口；当前产品消息路径走
  StartRun+StreamRun（Run 状态/Tool Call/停止保持一致）。
- 运行审批流（approval.request/responded）与 Jobs API 已映射，属 Hermes
  能力实现；Stage 6 的自动进度跟踪未实现。
