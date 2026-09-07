# mmdash Article Template Spec 1.0/1.1

正式 Article 模板是一个不可变 Artifact Version 指向的 ZIP。普通 Overleaf ZIP 只能经导入向导转换、验证并完成测试构建后注册。

## 必需内容

- `mmdash-template.json`
- manifest 指定的 TeX entrypoint
- 可选的 `cls/`、`sty/`、`bst/`、`assets/`、`fonts/`

manifest 必须符合 [`contracts/json-schema/article-template.schema.json`](../../contracts/json-schema/article-template.schema.json)，并声明 `schema_version`、`name`、`version`、`entrypoint`、`output`、`content_target`、`bibliography_target`、`engine` 和 `bibliography_tool`。`content_target` 与 `bibliography_target` 是系统生成文件的唯一写入位置，不能与 entrypoint 相同。

### Manifest 1.1 扩展字段（全部可选，1.0 模板继续有效）

- `abstract_target`：系统写入生成摘要块的 TeX 路径；摘要禁用时写入空文件，不产生空标题或占位。
- `body_layout`：`single`（仅 `content_target`）或 `sections`（Worker 按稳定 heading Block ID 把 H1/H2 拆分为 `sections/*.tex` 并生成按序引用它们的 `sections/body.tex`，`content_target` 即 `sections/body.tex`）。
- `field_profile`：`default`（通用字段）、`cumcm`（通用 + 国赛字段），或显式字段列表；未列入的字段不生成 TeX。
- `figure_dir`：构建时转换后图片的落位目录，默认 `figures`。
- `bibliography_mode`：`inline`（citeproc 把参考文献渲染进正文片段）或 `native`（保留模板自带 `\bibliography`/`\addbibresource` + `\printbibliography` 接线，系统只把 Zotero 文献写入 `bibliography_target`，Pandoc 输出 `\cite` 命令）。缺省 `inline`。

## Markdown → TeX 转换约定

正文与摘要由 Pandoc 以片段模式（无 `--standalone`）转换：输出不包含 `\documentclass`、preamble 或 `\begin{document}`，文档结构完全由模板 entrypoint 拥有，片段通过 `content_target`/`abstract_target` 被模板 `\input`。

- Pandoc 调用固定携带 `--no-highlight`：代码块一律输出标准 `verbatim` 环境。语法高亮只发生在编辑器内；不引入 Pandoc standalone 模板专属的 `Shaded`/`Highlighting` 宏依赖。
- `inline` 参考文献模式下，正文与摘要中的引用都由 citeproc 渲染为文本引用；`CSLReferences` 文献表只出现在正文片段中，摘要片段会被剥除文献表。
- 片段中允许出现的 Pandoc 生成命令以 worker 契约测试为准（`test_pandoc_fragment_stays_within_the_template_contract`）：升级 Worker 镜像的 Pandoc 版本时，新引入的模板专属命令会被该测试拦截。

## 安全验证

验证器拒绝绝对路径、`..`、反斜杠路径、NUL、重复成员、symlink、ZIP bomb、超额文件数/压缩或解压大小、脚本、Makefile、用户 `latexmkrc`、未登记的编译器、非法 entrypoint/output 以及覆盖模板原文件的生成目标。注册前使用受限测试正文执行一次同版本工具链构建。

模板构建不启用 shell escape，不访问网络，不执行模板脚本。正式 Build 固定 Artifact ID 和 Version ID；更新模板不会改变历史 Build 或 Release。

## 稳定错误码

- `ARTICLE_TEMPLATE_INVALID` / `ARTICLE_TEMPLATE_MANIFEST_INVALID`：ZIP 或 manifest 结构不合法。
- `ARTICLE_TEMPLATE_UNSAFE`：路径穿越、绝对路径、反斜杠、symlink 或重复成员。
- `ARTICLE_TEMPLATE_SCRIPT_FORBIDDEN`：发现脚本、Makefile、`latexmkrc` 或 executable bit。
- `ARTICLE_TEMPLATE_TARGET_EXISTS`：模板预先占用了系统生成目标。
- `ARTICLE_TOOLCHAIN_MISMATCH`：Worker 实际二进制与 Core 固定工具链不一致。
- `ARTICLE_BUILD_FAILED` / `ARTICLE_BUILD_TIMEOUT`：受限构建失败或超时；安全日志仍归档，已存在的 Commit 不回滚。

有效 manifest 示例见
[`article-template.valid.json`](../../contracts/json-schema/examples/article-template.valid.json)。
