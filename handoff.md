# mmdash v0.1 foundation handoff

更新时间：2026-07-28  
当前分支：`main`

## 下一个待开发模块
3.16 登录、注册与成员邀请闭环

## 观察到的问题
具体项目页面顶部项目名为项目id而不是项目名


## 当前结论

阶段 0 的 3.8–3.15 已实现，并且每个 3.x 节点都有独立提交。设计文档、
Dockerfile 修复、可检索 API 文档和 3.15 验收脚本均已纳入版本控制。

| 节点 | 提交 | 主要交付 |
| --- | --- | --- |
| 3.8 | `5e55cb2` | Auth、团队项目、成员、RBAC、Token 与 Web 项目流程 |
| 3.9 | `af315e0` | 类型化设置、AES-GCM Secret、连接测试与设置页插槽 |
| 3.10 | `9bea2f6` | OpenAPI/Schema 生成、兼容性基线、Mock 与模块脚手架 |
| 3.11 | `e548462` | PostgreSQL Job、租约、心跳、重试、取消与 Python Worker |
| 3.12 | `81cbb00` | Transactional Outbox、消费者投递、幂等与显式重放 |
| 3.13 | `940ddcd` | Data Hub 对象注册、时间线、上下文提案与 Home 聚合 |
| 3.14 | `4d0199e` | 追加式 Audit、脱敏日志、Prometheus 指标与版本信息 |
| 3.15 | `7b7d52f` | 全链路 smoke、Worker 容器、Compose 集成与静态验收 |

`docs/design/v0.1/README.md` 是当前设计文档索引。旧
`mmdash设计文档v0_1.md` 已由作者提供的新版本替代/重命名。

## 已完成的开发环境验收

- `node scripts/check-stage-3.15.mjs`
- Node 语法检查
- OpenAPI 与 JSON Schema 契约检查
- 契约兼容性基线检查
- API 目录覆盖检查：3 份契约、83 个 operation
- Caddyfile 入口不变量检查
- `go test ./...`
- `go vet ./...`
- Python Worker：10 个测试通过
- Ruff lint 与 format check

本机 `node_modules` 曾因中断的 pnpm 安装出现 workspace link/二进制缺失。
如需执行完整 TypeScript 测试，请先重新运行
`pnpm install --frozen-lockfile`，不要继续复用损坏的安装目录。

## API 文档入口

从 `docs/api/README.md` 开始检索：

- `docs/api/endpoints.md`：全部 HTTP operation 的可搜索目录；
- `docs/api/auth-projects.md`：Auth、Project、RBAC 与 Token；
- `docs/api/settings.md`：类型化配置、Secret 和连接测试；
- `docs/api/jobs.md`：Job/Worker 协议；
- `docs/api/events.md`：Outbox、消费者和重放；
- `docs/api/datahub.md`：Object Registry、Timeline、Context 与聚合；
- `docs/api/audit-observability.md`：Audit、request ID、日志和指标；
- `docs/api/mcp-tools.md`：MCP Tool；
- `docs/api/contracts.md`：契约生成和兼容性规则。

修改 API 后执行：

```powershell
pnpm.cmd contracts:generate
pnpm.cmd contracts:check
pnpm.cmd api:check
```

## Docker 验收结果

3.15 提交之后执行了完整 Docker 测试，符合阶段约束。2026-07-28 的最终
验收结果：

- PostgreSQL、MinIO、Core、Web BFF、Web、MCP Gateway 均为
  `running (healthy)`；
- Migration 容器成功应用 `000001`–`000008` 并以状态 0 退出；
- Worker 镜像通过 Compose 以一次性模式领取并完成 `system.test` Job；
- 完整 smoke 通过，输出：

```json
{
  "audit_events": 1,
  "event_id": "ad927fa3-9c91-41f9-9aac-0d8ab83ac057",
  "job_id": "ea10efbe-675b-4130-a7d8-7c306a18f2a1",
  "project_id": "df96ef83-c7b2-4978-ac50-93a2092cf883",
  "status": "passed"
}
```

首次 smoke 暴露并已修复两个仅真实 PostgreSQL 能发现的问题：

- Data Hub Project projector 在同一 prepared statement 中将 `$2` 同时推断
  为 UUID 和 text；现先固定为 UUID，再转换为 source ID text；
- Outbox 重试 SQL 的 `CASE` 时间参数缺少显式 `timestamptz` 类型，导致失败
  记录自身无法落库；
- smoke 现要求 Outbox 至少有一个成功 delivery，避免空数组的 `every()`
  产生假阳性。

`.dockerignore` 同步改为递归排除各语言缓存、虚拟环境和
`__pycache__`，避免这些本机目录进入 Docker build context。

交割时整栈仍保持运行，便于继续检查；如不再使用，执行下文的 `down`。

## Docker 复验流程

### 1. 构建并启动整栈

从 `I:\Project\mmdash` 执行：

```powershell
docker compose -f deploy/compose/compose.yaml build core migrate web-bff web mcp-gateway
docker compose -f deploy/compose/compose.yaml build worker
docker compose -f deploy/compose/compose.yaml up -d
docker compose -f deploy/compose/compose.yaml ps
```

所有长期服务应为 `running`/`healthy`，`migrate` 应为成功退出。

### 2. 执行 3.15 全链路 smoke

```powershell
$env:MMDASH_SMOKE_WORKER_MODE = "docker"
pnpm.cmd smoke
```

如果本机 pnpm 安装仍损坏，但 `clients/cli/dist/main.js` 已存在，可直接执行：

```powershell
$env:MMDASH_SMOKE_WORKER_MODE = "docker"
node scripts/smoke.mjs
```

成功结果会输出 JSON，包含 `status: "passed"`、`project_id`、`job_id`、
`event_id` 和 `audit_events`。该 smoke 覆盖：

- Web → BFF → Core → PostgreSQL；
- 浏览器登录、团队项目创建和 Home 聚合；
- Core 签发 Worker Token，Worker 领取并完成 `system.test` Job；
- Outbox 发布和消费者成功投递；
- Data Hub Project 对象与权威内容读取；
- 跨服务 `request_id` 及可查询 Audit；
- Core metrics、MCP health 和 CLI shell。

失败时收集：

```powershell
docker compose -f deploy/compose/compose.yaml ps
docker compose -f deploy/compose/compose.yaml logs --tail 200 core web-bff web mcp-gateway migrate
```

测试后保留数据卷、只停止容器：

```powershell
docker compose -f deploy/compose/compose.yaml down
Remove-Item Env:MMDASH_SMOKE_WORKER_MODE -ErrorAction SilentlyContinue
```

不要执行 `down -v`，除非明确决定删除本地 PostgreSQL/MinIO 测试数据。

## 拉起开发环境

### 工具和依赖

- Node.js 24（仓库 `.node-version`）；
- pnpm 11.9.0（根 `package.json#packageManager`）；
- Go 1.18+（当前模块仍兼容 Go 1.17）；
- Python 3.11+、uv；
- Docker Compose。

```powershell
Set-Location I:\Project\mmdash
corepack enable
pnpm.cmd install --frozen-lockfile
$env:UV_CACHE_DIR = "I:\Project\mmdash\.uv-cache"
uv sync --all-packages
```

只拉起开发依赖：

```powershell
docker compose -f deploy/compose/compose.yaml up -d postgres minio
docker compose -f deploy/compose/compose.yaml run --rm migrate
```

然后按以下文档在独立终端启动进程：

- Core：`docs/development/core.md`
- Web BFF：`docs/development/web-bff.md`
- Web：`docs/development/web.md`
- MCP Gateway：`docs/development/mcp-gateway.md`
- Worker：`docs/development/worker.md`
- CLI：`docs/development/cli.md`

常用入口：

```powershell
pnpm.cmd --filter @mmdash/web-bff dev
pnpm.cmd --filter @mmdash/web dev
pnpm.cmd --filter @mmdash/mcp-gateway dev
pnpm.cmd --filter @mmdash/cli dev -- --version
uv run --offline --package mmdash-worker mmdash-worker --status
```

本地进程使用 `localhost` 地址；`.env.example` 中的 `postgres`、`minio`、
`core` 主机名是 Compose 网络内地址，直接本地运行时应分别改成
`localhost:5432`、`localhost:9000`、`localhost:8080`。

## 后续开发注意事项

- 不要在下载等待上浪费token降低轮询频次，优先使用镜像而非代理
- 在commit前再进行docker测试，其他时候进行开发环境测试，数据库和redis的容器可提前拉起用于测试
- 当前阶段没有 Redis 服务；Job 使用 PostgreSQL
  `FOR UPDATE SKIP LOCKED`。在后续模块正式声明 Redis 前，不要增加隐式依赖。
- mmdash 是团队协作平台。项目成员、角色、权限和审计必须保持项目边界，
  不要退化为单用户本地工具模型。
- Core 是权威业务状态唯一所有者；BFF、MCP、Worker 不得直连业务表。
- 业务写入与 Outbox 必须在同一事务内；Worker 结果通过 Core Job API 回传。
- 新增/修改 HTTP API 时同步 OpenAPI、生成客户端、`docs/api/endpoints.md`
  和专题 API 文档。
- 务必告知开发者测试账号和密码
  - 账号：admin@mmdash.local
  - 密码：mmdash-local-admin

