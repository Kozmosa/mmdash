# mmdash v0.0 归档说明

此目录保存 v0.0 的设计文档、实施计划和历史操作说明，仅供参考，不再作为当前开发规范。

## 完整实现

v0.0 主线完整代码保存在：

- tag：`v0.0`
- branch：`archive/v0.0`
- 最终主线提交：`642628cf54c2b357438380f5c701b1d59dd3dda5`

远端还有两个未合入旧主线的功能分支，已建立对应的本地归档分支：

| 内容 | 归档分支 | 提交 |
| --- | --- | --- |
| Documosa 集成 | `archive/feat-documosa-v0.0` | `772d1bb6778c87e3b5f4cc07b50d1ce94ed23feb` |
| References Manager | `archive/feat-references-v0.0` | `c883610223a6f65b913890482c4b960e40e6d451` |

## 查看旧实现

推荐以只读参考 worktree 的方式查看：

```bash
git worktree add .worktrees/v0.0 archive/v0.0
```

也可以直接查看单个文件：

```bash
git show v0.0:backend/app/services/notion_provider.py
git show v0.0:backend/app/services/zotero_sync.py
git show v0.0:frontend/components/model/model-editor-shell.tsx
```

## 值得迁移的资产

- Notion Provider、块级 diff 与相关测试。
- Zotero 同步、BibTeX 和引用管理。
- 前端 UI 组件、Markdown 渲染、编辑器和落地页。
- 用户、团队、项目、提醒和通知模块的行为测试。

以下实现不应直接迁移：

- 浏览器直连本地 Agent 的执行链路。
- 任意 shell 和客户端传入本地绝对路径的接口。
- 进程内保存项目上下文的 Cloud Agent。
- 将 SQLite、Redis 或 Git 分支直接作为所有业务状态来源的做法。

## 文档

- `README.md`：旧版项目入口和架构说明。
- `PRD.md`：旧版产品需求。
- `stage1-plan.md`：旧版阶段计划。
- `CLAUDE.md`：旧版开发说明。
- `docs/`：旧版功能设计、测试材料和集成文档。
- `locks/`：归档时从本地开发环境保留下来的旧版 uv 依赖锁文件。

当前版本的权威设计位于 `docs/design/v0.1/`。
