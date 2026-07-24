# mmdash

mmdash v0.1 是面向数学建模与研究型项目的协作工作台。

当前主线处于架构重建阶段。v0.1 的设计基线位于：

- [mmdash 设计文档 v0.1](docs/design/v0.1/mmdash设计文档v0_1.md)

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
├── archive/
│   └── v0.0/                  # 旧版文档归档
├── docs/
│   └── design/
│       └── v0.1/              # 当前设计基线
├── .gitignore
└── README.md
```

后续实现应以 v0.1 设计为准。需要参照旧代码时，建议使用独立 worktree：

```bash
git worktree add .worktrees/v0.0 archive/v0.0
```

不要从归档 worktree 直接向 v0.1 主线提交代码；应按新架构迁移必要逻辑和测试。
