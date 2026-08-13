# Article component guide

Article 是 Stage 9 的 Core 领域模块，拥有协作草稿、块、Patch/Tag、固定引用、Commit、Preview、Build、模板、Zotero 绑定和不可变 Release。

## 所有权

- Web：Tiptap 编辑器、两栏写作体验、历史和只读 Release 详情。
- Web BFF：浏览器 Session、Project 权限、Hocuspocus/Yjs 房间、presence 与 commit flush 屏障。
- Core Article：PostgreSQL 权威状态、权限、Audit、Outbox、Job 编排和 Data Hub 读取适配器。
- Repo：article branch 的唯一 Git 读写入口。
- Artifact：模板 ZIP 与所有 Build 输出的不可变 Version。
- Worker：Job 范围内运行 Pandoc/citeproc、TeX Live 和 latexmk，不直接访问 DB、Git、对象存储或供应商 API。

## 开发验证

先运行 Article 所属 Go、BFF、Web 和 Worker 测试，再运行 `pnpm contracts:check`。完整阶段交付运行 `pnpm check` 以及 Compose build/smoke/down。Worker 镜像必须实际包含固定版本的 Pandoc、TeX Live 与 latexmk；构建命令使用参数数组、禁用 shell escape 和网络，并执行 CPU、内存、时间、磁盘和输出限制。

真实 Article 工具链验收在无网络的 Worker 容器中编译固定模板、Markdown、BibTeX 和 Artifact 图片，并检查全部不可变产物：

```powershell
docker compose -f deploy/compose/compose.yaml --profile worker build worker
pnpm smoke:article-worker
```

这条验收补充 `pnpm smoke`：整仓 smoke 验证 Core/BFF/Job 与 Worker 传输，`smoke:article-worker` 则实际执行 Pandoc、latexmk、BibTeX、pdfTeX 和 SyncTeX，不 mock 排版进程。

协作测试至少连接两个真实 WebSocket 客户端，并覆盖 viewer 只读、断线重连、Ctrl+S flush 和 Commit revision 隔离。Build 测试覆盖 preview latest-only、同 Commit 多 Build、失败保留 Commit、成功产物下载、Release 不可变和源码 ZIP 校验。
