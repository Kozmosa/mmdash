# Article collaboration and build security

Article WebSocket 只接受有效浏览器 Session 与 Project 上下文。BFF 限制单连接载荷、房间连接数、未认证排队消息和 awareness 大小；Viewer 的写入更新会被关闭并审计。

模板 ZIP 和 Build 输入都视为不可信。Worker 在无网络、非 root、固定工具链环境运行；只允许 `pdflatex`、`xelatex`、`lualatex` 以及 BibTeX/Biber；使用固定参数列表和 `-no-shell-escape`，不加载用户 `latexmkrc`，不执行脚本或 Makefile。日志仅保留相对路径和安全错误码，不记录 Zotero API Key、下载授权、绝对服务器路径或正文外的凭证。

Core 对 Job 输入与输出执行 Project、Job、Build 和 role 绑定校验。Worker 通过 Core 的 Job-scoped 接口取输入并流式回传；不能直接访问 PostgreSQL、Repo、MinIO/S3 或模型供应商。失败 Build 只追加失败记录，不覆盖成功 Artifact Version。
