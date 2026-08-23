# mmdash

mmdash v0.1 是面向数学建模与研究型项目的协作工作台。

当前主线处于架构重建阶段。v0.1 的设计基线位于：

- [`docs/design/v0.1`](docs/design/v0.1/)

工程、API 与架构入口：

- [本地开发](docs/development/README.md)
- [API 索引](docs/api/README.md)
- [架构索引](docs/architecture/README.md)

## 版本边界

- `v0.0`：旧版产品与实现，已冻结。
- `v0.1`：当前版本，围绕 Project Data Hub、外部 Agent、MCP/CLI、实验服务与无状态 Sandbox 重新设计。

旧版完整实现不复制到当前源码树中，保存在以下 Git 引用：

- tag：`v0.0`
- branch：`archive/v0.0`
- 未合并的旧功能分支：
  - `archive/feat-documosa-v0.0`
  - `archive/feat-references-v0.0`

旧版设计、计划和操作说明保存在 [archive/v0.0](archive/v0.0/)。

## 当前目录

```text
.
├── apps/                      # Web、BFF、MCP Gateway
├── clients/                   # 本地 CLI
├── packages/                  # TypeScript 共享包
├── contracts/                 # OpenAPI、事件与 JSON Schema
├── backend/                   # Go Core 模块化单体
├── workers/                   # Python 异步任务
├── box/                       # 独立能力节点
├── deploy/                    # 本地与单机部署
├── scripts/                   # 仓库级校验、smoke 与脚手架脚本
├── templates/                 # 模块脚手架模板
└── docs/                      # 架构、API、事件与开发文档
```

后续实现应以 v0.1 设计为准。需要参照旧代码时，建议使用独立 worktree：

```bash
git worktree add .worktrees/v0.0 archive/v0.0
```

不要从归档 worktree 直接向 v0.1 主线提交代码；应按新架构迁移必要逻辑和测试。

## 快速验证

```bash
pnpm install
uv sync --all-packages
pnpm check
```

完整本地示例链路见[本地开发文档](docs/development/README.md)。
