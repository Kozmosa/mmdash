import { strFromU8, strToU8, unzipSync, zipSync } from "fflate";

import type { ArticleTemplateManifest } from "./types";

const MAX_ARCHIVE_BYTES = 128 * 1024 * 1024;
const MAX_EXPANDED_BYTES = 512 * 1024 * 1024;
const MAX_FILES = 5_000;
const FORBIDDEN = /(^|\/)(?:makefile|latexmkrc|\.latexmkrc)$|\.(?:bat|cmd|com|dll|exe|jar|js|pl|ps1|py|rb|sh)$/i;

export type OverleafInspection = { candidates: string[]; fileCount: number; expandedBytes: number };

export async function inspectOverleafZip(file: File): Promise<OverleafInspection> {
  return inspectOverleafBytes(file.name, new Uint8Array(await readFile(file)));
}

export function inspectOverleafBytes(name: string, bytes: Uint8Array): OverleafInspection {
  const entries = readArchive({ name, size: bytes.byteLength }, bytes);
  const candidates = Object.entries(entries)
    .filter(([name, value]) => name.toLowerCase().endsWith(".tex") && decode(value).includes("\\begin{document}"))
    .map(([name]) => name)
    .sort((left, right) => (left === "main.tex" ? -1 : right === "main.tex" ? 1 : left.localeCompare(right)));
  if (!candidates.length) throw new Error("ZIP 中没有包含 \\begin{document} 的 TeX 主文件");
  return { candidates, fileCount: Object.keys(entries).length, expandedBytes: totalBytes(entries) };
}

export async function convertOverleafZip(
  file: File,
  entrypoint: string,
  fields: Pick<ArticleTemplateManifest, "name" | "version" | "engine" | "bibliography_tool">,
): Promise<{ file: File; manifest: ArticleTemplateManifest }> {
  const converted = convertOverleafBytes(file.name, new Uint8Array(await readFile(file)), entrypoint, fields);
  return {
    file: new File([converted.bytes.buffer.slice(converted.bytes.byteOffset, converted.bytes.byteOffset + converted.bytes.byteLength) as ArrayBuffer], `${basename(file.name, ".zip")}-mmdash.zip`, { type: "application/zip", lastModified: Date.now() }),
    manifest: converted.manifest,
  };
}

export function convertOverleafBytes(
  name: string,
  bytes: Uint8Array,
  entrypoint: string,
  fields: Pick<ArticleTemplateManifest, "name" | "version" | "engine" | "bibliography_tool">,
): { bytes: Uint8Array; manifest: ArticleTemplateManifest } {
  const entries = readArchive({ name, size: bytes.byteLength }, bytes);
  const source = entries[entrypoint];
  if (!source) throw new Error("所选 TeX 主文件不存在");
  if (entries["mmdash-template.json"]) throw new Error("该 ZIP 已包含 mmdash manifest，请按标准模板直接注册");
  const tex = decode(source);
  const begin = tex.indexOf("\\begin{document}");
  const end = tex.lastIndexOf("\\end{document}");
  if (begin < 0 || end <= begin) throw new Error("TeX 主文件缺少完整 document 环境");
  const contentTarget = ".mmdash/generated-content.tex";
  const bibliographyTarget = ".mmdash/references.bib";
  if (entries[contentTarget] || entries[bibliographyTarget]) throw new Error("ZIP 使用了 mmdash 保留的生成路径");
  const output = entrypoint.replace(/\.tex$/i, ".pdf");
  const manifest: ArticleTemplateManifest = {
    schema_version: "1.0", name: fields.name.trim(), version: fields.version.trim(),
    entrypoint, output, content_target: contentTarget,
    bibliography_target: bibliographyTarget, engine: fields.engine,
    bibliography_tool: fields.bibliography_tool,
  };
  if (!manifest.name || !manifest.version) throw new Error("模板名称和版本不能为空");
  const replacement = `${tex.slice(0, begin)}\\begin{document}\n\\input{${relativeTexPath(entrypoint, contentTarget)}}\n\\end{document}${tex.slice(end + "\\end{document}".length)}`;
  entries[entrypoint] = strToU8(replacement);
  entries["mmdash-template.json"] = strToU8(`${JSON.stringify(manifest, undefined, 2)}\n`);
  const converted = zipSync(Object.fromEntries(
    Object.entries(entries).map(([path, contents]) => [path, [contents, { level: 6 }]]),
  ));
  return { bytes: converted, manifest };
}

function readArchive(file: Pick<File, "name" | "size">, bytes: Uint8Array): Record<string, Uint8Array> {
  if (!file.name.toLowerCase().endsWith(".zip") || file.size < 1 || file.size > MAX_ARCHIVE_BYTES) throw new Error("请选择不超过 128 MiB 的 ZIP");
  let entries: Record<string, Uint8Array>;
  try { entries = unzipSync(bytes); } catch { throw new Error("ZIP 无法解压或已损坏"); }
  const names = Object.keys(entries);
  if (!names.length || names.length > MAX_FILES || totalBytes(entries) > MAX_EXPANDED_BYTES) throw new Error("ZIP 文件数量或解压大小超过限制");
  const folded = new Set<string>();
  for (const name of names) {
    if (!safePath(name) || FORBIDDEN.test(name)) throw new Error(`ZIP 包含不安全文件：${name}`);
    const key = name.toLowerCase();
    if (folded.has(key)) throw new Error(`ZIP 包含大小写冲突路径：${name}`);
    folded.add(key);
  }
  return entries;
}

function safePath(name: string) {
  return Boolean(name) && !name.startsWith("/") && !name.includes("\\") && !name.includes("\0") && !/^[A-Za-z]:/.test(name) && !name.split("/").includes("..");
}
function totalBytes(entries: Record<string, Uint8Array>) { return Object.values(entries).reduce((total, value) => total + value.byteLength, 0); }
function decode(value: Uint8Array) { try { return strFromU8(value); } catch { throw new Error("TeX 主文件不是 UTF-8 文本"); } }
function basename(name: string, suffix: string) { return name.toLowerCase().endsWith(suffix) ? name.slice(0, -suffix.length) : name; }
function relativeTexPath(entrypoint: string, target: string) {
  const depth = entrypoint.split("/").length - 1;
  return `${"../".repeat(depth)}${target.replace(/\.tex$/i, "")}`;
}
function readFile(file: File): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === "function") return file.arrayBuffer();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("ZIP 读取失败"));
    reader.onload = () => resolve(reader.result as ArrayBuffer);
    reader.readAsArrayBuffer(file);
  });
}
