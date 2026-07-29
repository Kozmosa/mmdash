# mmdash v0.1 Stage 1 Repo handoff

更新时间：2026-07-29
当前分支：`ModuleRepo`
已合入基线：`b230bd0`（`origin/main`，PR #23）

## 当前结论

Stage 1 Repo 已按 `docs/notion/GOrepo实现.md` 完成完整纵向切片，并通过
Native、Contract、Docker E2E、Smoke 和 `pnpm check` 验收。Core 仍是唯一
权威业务状态写入者；BFF、Web、MCP 和 Worker 均未绕过 Core 写业务表。

本阶段的检查点提交：

| 检查点 | 提交      | 主要交付                                                     |
| ------ | --------- | ------------------------------------------------------------ |
| 0      | `591c69b` | Repo OpenAPI、事件 Schema、生成客户端、ADR 与 API 目录       |
| 1      | `ace5e7f` | Migration `000012_repo`、持久化、安全 Git CLI 与受管存储     |
| 2      | `c9122f2` | GitHub/Local Provider、连接、三工作区映射、同步协调器与恢复  |
| 3      | `9f1b424` | 固定完整 SHA 的 Commit、Tree、Content 只读接口               |
| 4      | `ffd496c` | 临时 Checkout、受控 Commit/Push、`ArticleWorkspace` 能力接口 |
| 5      | `f666a66` | 签名 Webhook、幂等投递、Outbox、Data Hub 投影                |
| 6      | `337bccc` | BFF、Repo 设置页、只读浏览器与 MCP Data Hub 读取             |
| 7      | `d83b05c` | 延迟清理、Repo 指标、Git 最低版本与 Go 1.17 镜像兼容         |

## 完成范围

- 每个项目可绑定一个 GitHub HTTPS 或管理员允许目录中的 Local Git 仓库；
- `code`、`article`、`result` 映射到三个互不相同的实际分支，已有 `main`
  可映射为 `code`；
- Core 管理 Bare Repository、三个长期 Worktree、同步队列、错误恢复、
  并发租约和断开后的延迟清理；
- Commit、Tree 和 Content 的所有读取均固定到 40 位 Commit SHA；
- 文本、二进制、大文件、Git LFS pointer、symlink 和 submodule 使用安全、
  明确的只读响应；
- Core 内部支持固定 SHA Checkout、受控 Commit/Push 和
  `ArticleWorkspace`，Web/BFF 不暴露写入入口；
- GitHub Push Webhook 校验 HMAC，按 delivery 幂等处理，并覆盖删除分支和
  force-push；
- Repo 业务写入与 `repo.connected`、`repo.commit.detected`、
  `repo.commit.created` Outbox 事件保持同一事务；
- Data Hub 提供 Repository、Commit、Code File 投影，MCP `data.list` /
  `data.read` 通过 Core 做项目权限和审计读取；
- 设置页支持 Provider、PAT 脱敏、连接测试、工作区映射、状态、同步、
  Webhook Secret 轮换和断开；
- 求解记录页支持工作区切换、提交列表/详情、懒加载 ARIA 文件树、完整 SHA
  URL 固定和只读 Monaco 预览；
- readiness 检查 PostgreSQL、对象存储、Git `>= 2.20.0` 和 Repo 存储；
- Prometheus 暴露低基数 Repo operation、duration、sync queue、checkout 和
  storage 指标。

Stage 1 不实现 Artifact 大文件、Experiment 业务、Article 产品模块或另一套
CLI Git 管理逻辑；这些边界按设计保留。

## 公共接口与运行配置

API 与运行说明从以下文档进入：

- `docs/api/repo.md`
- `docs/development/repo.md`
- `docs/api/endpoints.md`
- `docs/api/events.md`
- `docs/api/datahub.md`
- `docs/api/mcp-tools.md`

新增 Migration：

- `backend/migrations/000012_repo.up.sql`
- `backend/migrations/000012_repo.down.sql`

关键配置：

- `REPO_STORAGE_ROOT`
- `REPO_LOCAL_ALLOWED_ROOTS`
- `REPO_MAX_CONCURRENT_GIT`
- `REPO_GIT_TIMEOUT`
- `REPO_SYNC_POLL_INTERVAL`
- `REPO_SYNC_LEASE`
- `REPO_CHECKOUT_TTL`
- `REPO_CLEANUP_GRACE`
- `MMDASH_SECRET_KEY`

GitHub PAT 仅通过临时 askpass 环境注入 Git 子进程；API、日志、事件和 Data Hub
不返回 Secret 或服务器内部检出路径。

## 最终验证

2026-07-29 的最终结果：

- `pnpm contracts:generate`：通过，生成文件新鲜；
- `pnpm contracts:check`：2 份 OpenAPI 与共享 Schema 通过，兼容性基线通过；
- `pnpm api:check`：5 份契约、139 个 operation 全部有目录条目；
- `pnpm lint`：ESLint、Go format、Ruff 全部通过；
- `go test ./...`：全部通过，Repo 套件约 31 秒；
- `pnpm check`：退出码 0；全部 lint、TS/Go/Python 测试、构建、契约、API
  和 Caddyfile 校验通过；
- Python Worker：13/13 通过，无跳过；仅有不可写 `.pytest_cache` 的非功能性
  warning；
- Caddy `v2.11.4`：官方 SHA512 校验后执行，`Valid configuration`；
- `docker-compose ... config --quiet`：通过；
- Core Dockerfile 使用实际 Go 1.17 基础镜像构建通过；Stage 1 没有提前处理
  独立 Issue #19 的仓库级最低 Go 版本决策。

Docker Compose 最终长期服务均为 `healthy`：

- PostgreSQL 16
- MinIO
- Core
- Web BFF
- Web
- MCP Gateway

Migration 容器成功应用 `000012_repo` 并以状态 0 退出。Core readiness：

```json
{
  "dependencies": {
    "git": "ready",
    "object_storage": "ready",
    "postgres": "ready",
    "repo_storage": "ready"
  },
  "status": "ready"
}
```

近期 Core、BFF、Web、MCP 日志未发现 panic/fatal/error，也未发现 PAT、
Webhook Secret、Git token 或 Authorization header 输出。

## Docker Repo E2E 与 UI 验收

最终命令：

```powershell
$env:REPO_LOCAL_ALLOWED_ROOTS = "/tmp"
$env:MMDASH_SMOKE_WORKER_MODE = "docker"
$env:MMDASH_SMOKE_REPO_MODE = "docker"
$env:MMDASH_SMOKE_COMPOSE_COMMAND = "docker-compose"
pnpm smoke
```

Smoke 使用 Core 容器内唯一 Local Git fixture，覆盖：

- 浏览器登录与项目创建；
- Local Repo 保存和连接测试；
- `main/article/result` 三工作区绑定；
- 首次同步和三工作区 HEAD；
- 固定 SHA 的 Commit、Tree、Content；
- 固定 SHA Checkout 创建与释放，且不泄露服务器路径；
- 外部提交、手工同步、旧 SHA 不变和新 SHA 内容更新；
- Repo Commit Data Hub 投影与权威读取；
- Worker Job、Outbox、Audit、metrics、MCP health 和 CLI 启动。

最终输出：

```json
{
  "audit_events": 1,
  "event_id": "4667797c-c7ee-4937-85b4-cf91e3b186bf",
  "job_id": "3501a0bb-2d00-4fb5-a418-1e605bc66a26",
  "project_id": "e44fb991-a7e7-4c3f-8d26-d115a13ca8e6",
  "repo": {
    "code_head": "621cb60729899701462ba1ce9a46e9b974b820aa",
    "detected_head": "660d2198cc58750507c363b2b53ae409680b5099",
    "project_id": "6c037298-f6bf-4506-8509-8ae07d24a816",
    "repository_id": "95d9dc8f-b213-4423-8dae-2f5d108f5756"
  },
  "status": "passed"
}
```

Local Provider 没有 GitHub Webhook，因此 Docker fixture 使用手工同步模拟远端
推进；HMAC、幂等、分支删除、force-push 和 BFF 原始 body 转发由 Go/BFF 测试
覆盖。

真实 Chrome 登录后的 UI 截图已目视复核：

- `docs/screenshots/repo-settings.png`
- `docs/screenshots/repo-browser.png`

设置页、完整 SHA、提交列表、文件树和 Monaco 内容均正确，无加载残留、遮挡或
水平溢出。

## 2026-07-29 Base 冲突解决复验

`ModuleRepo` 已合入 `origin/main` 的 `b230bd0`。Project 路由同时保留 Repo
`/repository` 与基线新增的 `/restore`，测试同时覆盖 Repo 协作角色权限和项目
回收站/恢复行为；开发文档保留新版基础说明并追加 Repo smoke 指引。

基线新增 `000010_invitation_decline` 和 `000011_project_trash` 后，Repo Migration
顺延为 `000012_repo`。为兼容已经运行过开发分支旧名 `000010_repo` 的保留数据卷，
`000012_repo.up.sql` 对同一 Repo 表和索引使用幂等创建；验证结果：

- 保留数据卷成功补记 `000012_repo`，原 Repo 数据无需重建，Migration 容器退出 0；
- 独立全新数据库依次成功应用 `000010_invitation_decline`、
  `000011_project_trash`、`000012_repo`，随后删除该临时数据库；
- `pnpm check` 完整退出 0：Web 36/36、Web BFF 25/25、MCP Gateway 10/10、
  Python Worker 13/13，以及全部 Go/其他 TypeScript 测试、lint、构建、契约、
  139 个 API operation 和 Caddy 校验均通过，无测试跳过；
- Compose 常驻服务全部 `healthy`，Migration 容器退出 0；Core readiness 的 Git、
  PostgreSQL、Object Storage 和 Repo Storage 均为 `ready`；
- 最近服务日志未发现 panic、fatal、error 或凭据输出。

合并后 Docker Repo smoke 再次通过：

```json
{
  "audit_events": 1,
  "event_id": "cb09867a-12dc-4119-8ba9-b291aa101ef8",
  "job_id": "276f221a-82b9-4304-b2b9-0d924634bce8",
  "project_id": "4f31ed5f-13d9-4bad-8873-d17ccc9b712f",
  "repo": {
    "code_head": "d4bcb10dd69e93a1bfbb3ef5c54afad093ca0f2e",
    "detected_head": "d2a3233d53bfc1d3ee959f9ecad70fc4615ad7f5",
    "project_id": "a33a445d-e479-42ce-a3ab-b7de7651ef04",
    "repository_id": "115f2a34-b723-40e6-8625-a12093a6fa3e"
  },
  "status": "passed"
}
```

## 复验与停止

完整复验：

```powershell
pnpm contracts:generate
pnpm check

$env:REPO_LOCAL_ALLOWED_ROOTS = "/tmp"
docker-compose -f deploy/compose/compose.yaml up -d --build
$env:MMDASH_SMOKE_WORKER_MODE = "docker"
$env:MMDASH_SMOKE_REPO_MODE = "docker"
$env:MMDASH_SMOKE_COMPOSE_COMMAND = "docker-compose"
pnpm smoke
```

测试后保留数据卷、只停止容器：

```powershell
docker-compose -f deploy/compose/compose.yaml down
```

不要执行 `down -v`，除非明确批准删除 PostgreSQL/MinIO 测试数据。

本地测试账号：

- 账号：`admin@mmdash.local`
- 密码：`mmdash-local-admin`

## 已知问题与下一阶段

- Issue #22 仍跟踪 Worker 测试可靠性；本次完整 Python 套件实际通过，但该问题
  不属于 Repo 范围；
- Issue #19 仍跟踪 Go 最低版本统一；本次只保证新增 Repo 代码能在现有 Go 1.17
  Core Docker 镜像中构建；
- 当前没有 Redis；Job 继续使用 PostgreSQL `FOR UPDATE SKIP LOCKED`。

单一推荐下一阶段：**Stage 2 Artifact**。先评审权威设计并冻结 Artifact
OpenAPI/Schema，不要让后续模块各自创建文件表或绕过 Core/Object Storage
边界。
