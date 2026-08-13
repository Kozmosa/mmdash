# ADR 0004: Article 协作协议、持久化与提交屏障

- 状态：Accepted
- 日期：2026-08-13
- 决策阶段：v0.1 Stage 9

## 背景

Article 需要多人实时写作、离线重连、光标与选区、AI Patch 审阅以及可证明一致的 Git Commit。自行实现 OT/CRDT 或让 BFF 直接访问数据库都会破坏既有 Core 所有权边界。

## 决策

浏览器使用 Tiptap Collaboration、Yjs、Hocuspocus Provider、Collaboration Caret 与 UniqueID。UniqueID 在 Collaboration 前初始化；StarterKit 的 UndoRedo 在协作模式关闭，历史由 Yjs 管理。

现有 Fastify Web BFF 承载单实例 Hocuspocus 边界，负责浏览器 Session、Project 权限、房间、presence、连接/载荷限制和 WebSocket 生命周期。BFF 不访问 PostgreSQL、Git 或对象存储；Hocuspocus 的加载、保存和 flush 钩子只调用生成的 Core Client。

Core 拥有 draft revision、Yjs document update/state vector、规范 Tiptap JSON、稳定 block_id、块投影和 Markdown 投影。PostgreSQL 保存协作快照和修订。v0.1 不引入 Redis；多 BFF 实例间同步延后到 Stage 10。

Commit 使用以下一致性屏障：

1. BFF 从当前 Y.Doc 固定 update 与 state vector，并调用 Core flush；
2. Core CAS 固定新的 draft revision，校验/生成 Tiptap JSON、块和 Markdown 投影；
3. Article 用该 revision 的 `manuscript.md`、冻结引用和 manifest 调用 Repo `ArticleWorkspace.Commit`；
4. 后续 Yjs 编辑产生更高 revision，不得进入既有 Commit。

AI Patch 只可作为带 actor/provenance 的 Yjs transaction 被接受；不得直接覆盖数据库正文。Viewer 连接为只读，Editor/Owner/Maintainer 可写；Agent 只能提出 Patch，Worker 仅持有 Job 范围能力。

## 后果

- Ctrl+S 等价于显式 flush，不等价于 Git Commit。
- 未提交协作草稿不会产生 Repo Commit。
- reconnect 使用服务端 Yjs 快照合并，UI 显示同步中、已同步、离线和失败状态。
- WebSocket 测试必须包含两个真实客户端、权限拒绝与断线重连。
