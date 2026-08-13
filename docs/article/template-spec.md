# mmdash Article Template Spec 1.0

正式 Article 模板是一个不可变 Artifact Version 指向的 ZIP。普通 Overleaf ZIP 只能经导入向导转换、验证并完成测试构建后注册。

## 必需内容

- `mmdash-template.json`
- manifest 指定的 TeX entrypoint
- 可选的 `cls/`、`sty/`、`bst/`、`assets/`、`fonts/`

manifest 必须符合 [`contracts/json-schema/article-template.schema.json`](../../contracts/json-schema/article-template.schema.json)，并声明 `schema_version`、`name`、`version`、`entrypoint`、`output`、`content_target`、`bibliography_target`、`engine` 和 `bibliography_tool`。`content_target` 与 `bibliography_target` 是系统生成文件的唯一写入位置，不能与 entrypoint 相同。

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
