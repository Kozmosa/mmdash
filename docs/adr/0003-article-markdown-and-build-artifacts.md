# ADR 0003: Article 以 Markdown 为唯一可编辑源，生成物归档为 Artifact

- 状态：Accepted
- 日期：2026-08-13
- 决策阶段：v0.1 Stage 9

## 背景

早期 v0.1 文档曾把 Markdown 与生成的 LaTeX 一起描述为 article 分支内容，也曾暗示可在产品内继续编辑生成的 TeX。这会产生两套可编辑真相，无法明确 Commit、Build 与 Release 的可复现边界。

## 决策

Article 只允许编辑协作 Markdown。一次 Article Commit 通过 Repo `ArticleWorkspace` 只写入：

- `manuscript.md`
- `references.bib`
- `.mmdash/article.json`

Pandoc/citeproc 生成的 LaTeX、PDF、完整源码 ZIP、Build Report、日志和 SyncTeX 均为不可变 `kind=article_build`、`source=article` Artifact Version，不进入 Git。正式 Build 必须绑定不可变 Commit；草稿 Preview 只绑定已持久化 draft revision，采用 latest-only 保留策略，不能创建 Release。

Release 固定一个 Article Commit、一次成功的正式 Build、模板 Artifact Version、引擎与工具链元数据。Release 创建后不可修改。需要深度修改 TeX 时，用户下载完整源码 ZIP 并导入 Overleaf；mmdash 不提供 TeX 在线编辑或 LaTeX 回退分支。

## 后果

- Git 历史只表达作者可编辑意图，Build 历史表达工具链输出。
- 同一 Commit 可以有零到多个 Build；失败 Build 不覆盖成功 Build。
- 源码 ZIP 必须包含可复现清单、校验和与固定工具链信息。
- 写作页面采用“可折叠/可调整左侧参考区 + 右侧大面积 Markdown 编辑器”；成功 Build/Release 详情采用只读“TeX 文件树 | TeX | PDF”布局。
