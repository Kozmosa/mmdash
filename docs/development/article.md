# Article component guide

Article 是 Stage 9 的 Core 领域模块，拥有协作草稿、块、Patch/Tag、固定引用、Commit、Preview、Build、模板、Zotero 绑定和不可变 Release。

## 写作工作流与手测

1. 在 Project Settings 的 Repo 区连接仓库并确认 Article 分支映射；未配置或分支未就绪时，Commit 返回稳定的 `ARTICLE_REPOSITORY_NOT_CONFIGURED` / `ARTICLE_REPOSITORY_NOT_READY` 409 错误，Web 会给出设置入口。
2. 首次读取 Article 聚合时，Core 会为新旧项目幂等安装受保护的 `mmdash` 默认 XeLaTeX 模板并创建模板测试 Build。用户无需进入模板页或上传 ZIP；模板页仍可导入 Overleaf ZIP、登记标准模板 Artifact Version，或将内置模板复制为普通 Artifact 后自定义。若模板测试尚未进入 `ready`，Commit 对话框会显示明确状态，但仍允许“仅提交”。
3. 写作页支持块拖拽手柄、`/` 命令、代码块、GFM 表格和 KaTeX 公式。Artifact 卡可拖到编辑器；系统先按 immutable Artifact Version 幂等固定引用，再插入 `artifactReference` 块。相邻普通图片或图片 Artifact 可合并为一个 `articleImageGroup` 块：组合块支持 1–4 张/行并自动换行（最多 16 张），每个子图保留独立图注，组合块另有位于整组下方的大题注；拆分、移出或删除子图不会丢失其固定版本信息。
4. 保存同步后检查“章节与块 tags”：新块标为 draft，已存在且内容改变的块自动标为 revision；人工点击“审阅”会写入审阅人/时间并产生 `article.block.reviewed` 审计与 Outbox 事件。章节状态独立持久化；未改动的 reviewed 块在后续 flush 中保留，重新编辑则回到 revision。
5. `Commit…` 先强制刷新 Yjs 房间，再由 Repo 的 `ArticleWorkspaceService` 写入唯一三个可编辑文件。可选择“提交并发布”，按 Commit → formal Build → Release 执行；Build 失败保留 Commit，可从版本历史重试同一固定 Commit。草稿预览与正式 Build 都显示由 Worker 回报的真实阶段进度（准备模板、整理资源、生成 TeX、编译 PDF、打包源码、归档产物），而不是用前端模拟百分比。
6. Zotero 凭据在 Project Settings 的 Article · Zotero 区配置。公开字段与加密 API key 均由 Settings Registry 管理，读取时只返回脱敏占位；写作页仅执行只读搜索并固定条目版本。
7. 成功 Release 固定关联正式 Build 的不可变输出。Release 详情可预览 PDF，并下载完整的 TeX 源码 ZIP；源码包包含入口 TeX、参考文献、模板内容、资源文件和构建清单，可用于离线复现。

手测 Issue #41 时至少验证：同一块拖动排序后 ID 不变；重复拖入同一 Artifact Version 只产生一个引用；数学公式可视化且 Markdown 投影使用 `$`/`$$`；表格、代码与 `/` 菜单可用；reviewed 块编辑后变为 revision；未配置 Repo 和缺少模板均有可执行引导；Zotero key 从不回显明文。

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

协作测试至少连接两个真实 WebSocket 客户端，并覆盖 viewer 只读、断线重连、Ctrl+S flush、块排序和 Commit revision 隔离。Build 测试覆盖 preview latest-only、同 Commit 多 Build、失败保留 Commit、成功产物下载、Publication retry 路由、Release 不可变和源码 ZIP 校验。
