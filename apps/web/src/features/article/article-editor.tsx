"use client";

import type { HocuspocusProvider } from "@hocuspocus/provider";
import type { Editor } from "@tiptap/core";
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import DragHandle from "@tiptap/extension-drag-handle-react";
import { Mathematics } from "@tiptap/extension-mathematics";
import {
  isNodeRangeSelection,
  NodeRange,
  NodeRangeSelection,
} from "@tiptap/extension-node-range";
import { TableKit } from "@tiptap/extension-table";
import {
  Fragment,
  Slice,
  type Node as ProseMirrorNode,
  type Schema,
} from "@tiptap/pm/model";
import { NodeSelection, TextSelection } from "@tiptap/pm/state";
import { TableMap } from "@tiptap/pm/tables";
import UniqueID from "@tiptap/extension-unique-id";
import { EditorContent, useEditor } from "@tiptap/react";
import { useQuery } from "@tanstack/react-query";
import StarterKit from "@tiptap/starter-kit";
import {
  Bold,
  Captions,
  Code2,
  GripVertical,
  Heading2,
  ImagePlus,
  Italic,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  Plus,
  Save,
  Sigma,
  Table2,
  Undo2,
  X,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useState,
  type ChangeEvent,
  type ClipboardEvent,
  type DragEvent,
  type MouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useRef,
} from "react";
import { createPortal } from "react-dom";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  artifactApi,
  ensureArtifactRootFolder,
} from "@/features/artifact/artifact-api";
import { MultipartUploadTask } from "@/features/artifact/multipart-upload";
import { optionalRequest } from "@/features/repo/optional-request";
import { apiClient } from "@/lib/api-client";

import { createArticleNodes } from "./article-nodes";
import {
  convertArticleBlock,
  deleteArticleBlock,
  deleteSelectedArticleBlock,
  duplicateArticleBlock,
  insertArticleBlock,
  moveArticleBlock,
  moveArticleBlockRange,
  replaceArticleImageWithArtifact,
  selectArticleBlock,
  type ArticleBlockConversion,
} from "./article-block-commands";
import { ArticleBlockMenu } from "./article-block-menu";
import {
  dropIndicatorOffset,
  dropTargetPosition,
  moveArrayItem,
  rectangleFromPoints,
  rectanglesIntersect,
  wheelScrollDelta,
  type EditorRectangle,
} from "./article-editor-interactions";
import {
  ArticleNodeMenu,
  type ArticleImageAlignment,
  type ArticleNodeMenuKind,
  type TableAction,
} from "./article-node-menu";
import {
  articleImageGroupContext,
  deleteArticleImageNode,
  insertArticleImageIntoGroup,
  isArticleImageNode,
  mergeArticleImageWithNeighbor,
  moveArticleImageInGroupDirection,
  normalizeArticleImageGroupColumns,
  removeArticleImageFromGroup,
  ungroupArticleImages,
  type ArticleImageGroupAction,
} from "./article-image-group";
import { articleBlockContentFingerprint } from "./article-block-fingerprint";
import {
  ArticleTableEdgeControls,
  type ArticleTableEdgeAction,
  type ArticleTableEdgeHandle,
} from "./article-table-edge-controls";
import { ArticleTagRail } from "./article-tag-rail";
import {
  isTransientImageURL,
  normalizeImageWidth,
} from "./article-image-utils";
import { ArticleMathEditor } from "./article-math-editor";
import {
  ArticleMathShortcuts,
  type ArticleMathKind,
} from "./article-math-shortcuts";
import { openArticleImageUploadEvent, SlashCommand } from "./slash-command";
import {
  ARTICLE_RENDER_THEME_EVENT,
  type ArticleBlock,
  type ArticleChapterTag,
  type ArticleRenderTheme,
} from "./types";

export const articleArtifactMime =
  "application/vnd.mmdash.article-artifact+json";
export const articleZoteroMime = "application/vnd.mmdash.zotero+json";
export const articleOutlineNavigateEvent = "mmdash:article-outline-navigate";
export const articleOutlineActiveEvent = "mmdash:article-outline-active";
export const articleInsertArtifactIntoGroupEvent =
  "mmdash:article-insert-artifact-into-group";

type TableCellRecord = {
  colspan: number;
  node: ProseMirrorNode;
  startCol: number;
};

const articleTableDefaultCellWidth = 100;
const articleTableMinimumCellWidth = 25;

function articleTableCellWidth(
  cell: TableCellRecord,
  offset: number,
  fallback: number,
) {
  const width = cell.node.attrs.colwidth?.[offset];
  return typeof width === "number" && Number.isFinite(width) && width > 0
    ? width
    : fallback;
}

export function equalizedArticleTableCellWidths(
  table: ProseMirrorNode,
): Map<number, number[]> {
  const map = TableMap.get(table);
  const cells = new Map<number, TableCellRecord>();
  for (let row = 0; row < map.height; row += 1) {
    for (let col = 0; col < map.width; col += 1) {
      const position = map.map[row * map.width + col];
      if (cells.has(position)) continue;
      const node = table.nodeAt(position);
      if (!node) continue;
      cells.set(position, {
        colspan: Math.max(1, Number(node.attrs.colspan ?? 1)),
        node,
        startCol: col,
      });
    }
  }

  if (map.width === 0 || map.height === 0 || cells.size === 0) return new Map();

  const columnWidths = Array.from({ length: map.width }, (_, col) => {
    const widths = [...cells.values()]
      .filter(
        (cell) => col >= cell.startCol && col < cell.startCol + cell.colspan,
      )
      .map((cell) => articleTableCellWidth(cell, col - cell.startCol, 0))
      .filter((width) => width > 0);
    return widths.length
      ? widths.reduce((sum, width) => sum + width, 0) / widths.length
      : articleTableDefaultCellWidth;
  });
  const result = new Map<number, number[]>();

  const width = Math.max(
    articleTableMinimumCellWidth,
    Math.round(columnWidths.reduce((sum, value) => sum + value, 0) / map.width),
  );
  for (const [position, cell] of cells) {
    result.set(position, Array(cell.colspan).fill(width));
  }
  return result;
}
export const articleUploadImageIntoGroupEvent =
  "mmdash:article-upload-image-into-group";

export function sameArticleOutline(
  left: ArticleOutlineItem[],
  right: ArticleOutlineItem[],
): boolean {
  return (
    left.length === right.length &&
    left.every(
      (item, index) =>
        item.id === right[index]?.id &&
        item.level === right[index]?.level &&
        item.text === right[index]?.text,
    )
  );
}

export function sameTagPositions(
  left: Record<string, number>,
  right: Record<string, number>,
): boolean {
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every((key) => Math.abs(left[key]! - (right[key] ?? NaN)) < 0.5)
  );
}

export function migrateLegacyTableCaptions(editor: Editor): boolean {
  const pairs: Array<{
    captionPos: number;
    captionSize: number;
    tablePos: number;
    tableAttrs: Record<string, unknown>;
  }> = [];
  editor.state.doc.forEach((node, offset, index) => {
    const next = editor.state.doc.maybeChild(index + 1);
    if (node.type.name !== "tableCaption" || next?.type.name !== "table")
      return;
    pairs.push({
      captionPos: offset,
      captionSize: node.nodeSize,
      tablePos: offset + node.nodeSize,
      tableAttrs: {
        ...next.attrs,
        caption: String(next.attrs.caption ?? "").trim() || node.attrs.caption,
      },
    });
  });
  if (!pairs.length) return false;
  const transaction = editor.state.tr;
  for (const pair of pairs.reverse()) {
    transaction.setNodeMarkup(
      pair.tablePos,
      editor.schema.nodes.table,
      pair.tableAttrs,
    );
    transaction.delete(pair.captionPos, pair.captionPos + pair.captionSize);
  }
  transaction.setMeta("addToHistory", false);
  editor.view.dispatch(transaction);
  return true;
}
export const articleInsertReferenceEvent = "mmdash:article-insert-reference";

export type ArticleOutlineItem = {
  id: string;
  level: number;
  text: string;
};

export type ArticleArtifactDrop = {
  artifactId: string;
  filename: string;
  mimeType: string;
  title: string;
  versionId: string;
};

export type ArticleVersionedReferenceInsert = {
  mimeType?: string;
  objectId: string;
  referenceId: string;
  referenceType: "experiment_result" | "model_snapshot" | "problem";
  title: string;
  versionId: string;
};

type ImageReplacementTarget = {
  blockId: string;
  fallbackPosition: number;
};

type TableDragIndicator = {
  axis: "column" | "row";
  left: number;
  length: number;
  top: number;
};

type ArticleDropIndicator = {
  label: string;
  position: number;
  top: number;
};

type TableDragSession = {
  active: boolean;
  handle: ArticleTableEdgeHandle;
  startX: number;
  startY: number;
  targetIndex: number;
};

function pointIsOverEditableText(
  editorRoot: HTMLElement,
  clientX: number,
  clientY: number,
): boolean {
  const range = document.caretRangeFromPoint?.(clientX, clientY);
  if (!range || !editorRoot.contains(range.startContainer)) return false;
  const text = range.startContainer;
  if (text.nodeType !== Node.TEXT_NODE || !text.textContent?.length)
    return false;
  const offset = Math.min(range.startOffset, text.textContent.length - 1);
  const probe = document.createRange();
  probe.setStart(text, Math.max(0, offset));
  probe.setEnd(text, Math.min(text.textContent.length, offset + 1));
  return Array.from(probe.getClientRects()).some(
    (rect) =>
      clientX >= rect.left - 2 &&
      clientX <= rect.right + 2 &&
      clientY >= rect.top - 2 &&
      clientY <= rect.bottom + 2,
  );
}

export function parseArticleArtifactDrop(raw: string): ArticleArtifactDrop {
  const value = JSON.parse(raw) as Partial<ArticleArtifactDrop>;
  if (
    ![
      value.artifactId,
      value.versionId,
      value.title,
      value.filename,
      value.mimeType,
    ].every((item) => typeof item === "string" && item.length > 0)
  ) {
    throw new Error("Artifact 拖拽数据不完整");
  }
  return value as ArticleArtifactDrop;
}

export function droppedLocalImage(
  dataTransfer: Pick<DataTransfer, "files" | "types">,
): File | undefined {
  const types = Array.from(dataTransfer.types);
  if (
    types.includes(articleArtifactMime) ||
    types.includes(articleZoteroMime) ||
    types.includes("application/vnd.mmdash.image-group-item")
  ) {
    return undefined;
  }
  return Array.from(dataTransfer.files).find((item) =>
    item.type.startsWith("image/"),
  );
}

export type ArticleZoteroDrop = {
  citationKey: string;
  itemKey: string;
  raw: Record<string, unknown>;
  title: string;
  version: number;
};

export function parseArticleZoteroDrop(raw: string): ArticleZoteroDrop {
  const value = JSON.parse(raw) as Partial<ArticleZoteroDrop> & {
    citation_key?: unknown;
    item_key?: unknown;
  };
  const citationKey = value.citationKey ?? value.citation_key;
  const itemKey = value.itemKey ?? value.item_key;
  if (
    typeof itemKey !== "string" ||
    !itemKey.trim() ||
    typeof value.title !== "string" ||
    !value.title.trim() ||
    typeof citationKey !== "string" ||
    !citationKey.trim() ||
    typeof value.version !== "number"
  ) {
    throw new Error("Zotero 条目拖拽数据不完整");
  }
  return {
    citationKey: citationKey.trim(),
    itemKey: itemKey.trim(),
    title: value.title.trim(),
    version: value.version,
    raw:
      value.raw && typeof value.raw === "object"
        ? (value.raw as Record<string, unknown>)
        : {},
  };
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

export function transformMathInHtml(html: string): string {
  let result = html.replace(
    /\$\$\$\$([\s\S]+?)\$\$\$\$/g,
    (_, latex) =>
      `<div data-type="blockMath" data-latex="${escapeHtml(latex.trim())}">$$${escapeHtml(latex.trim())}$$</div>`,
  );
  result = result.replace(
    /(?<!\$)\$\$([\s\S]+?)\$\$(?!\$)/g,
    (_, latex) =>
      `<div data-type="blockMath" data-latex="${escapeHtml(latex.trim())}">$$${escapeHtml(latex.trim())}$$</div>`,
  );
  result = result.replace(
    /\\\[([\s\S]+?)\\\]/g,
    (_, latex) =>
      `<div data-type="blockMath" data-latex="${escapeHtml(latex.trim())}">$$${escapeHtml(latex.trim())}$$</div>`,
  );
  result = result.replace(
    /(?<![$\w\\])\$(?!\$)([^$\n\r]+?)(?<!\\)\$(?![$\w])/g,
    (_, latex) =>
      `<span data-type="inlineMath" data-latex="${escapeHtml(latex.trim())}">$${escapeHtml(latex.trim())}$</span>`,
  );
  result = result.replace(
    /\\\(([\s\S]+?)\\\)/g,
    (_, latex) =>
      `<span data-type="inlineMath" data-latex="${escapeHtml(latex.trim())}">$${escapeHtml(latex.trim())}$</span>`,
  );
  return result;
}

export function parseTextWithMath(
  text: string,
  schema: Schema,
): ProseMirrorNode[] {
  const blockMathType = schema.nodes.blockMath;
  const inlineMathType = schema.nodes.inlineMath;
  const paragraphType = schema.nodes.paragraph;

  if (!blockMathType && !inlineMathType) return [];

  const normalized = text.replace(/\r\n/g, "\n");
  const trimmed = normalized.trim();

  // If the entire text is a single block equation:
  if (
    (trimmed.startsWith("$$$$") &&
      trimmed.endsWith("$$$$") &&
      trimmed.length >= 8) ||
    (trimmed.startsWith("$$") &&
      trimmed.endsWith("$$") &&
      trimmed.length >= 4 &&
      !trimmed.slice(2, -2).includes("$$")) ||
    (trimmed.startsWith("\\[") &&
      trimmed.endsWith("\\]") &&
      trimmed.length >= 4)
  ) {
    let latex = "";
    if (trimmed.startsWith("$$$$")) latex = trimmed.slice(4, -4).trim();
    else if (trimmed.startsWith("$$")) latex = trimmed.slice(2, -2).trim();
    else if (trimmed.startsWith("\\[")) latex = trimmed.slice(2, -2).trim();
    if (blockMathType) {
      return [blockMathType.create({ latex })];
    }
  }

  // If the entire text is a single inline equation:
  if (
    (trimmed.startsWith("$") &&
      trimmed.endsWith("$") &&
      trimmed.length >= 2 &&
      !trimmed.slice(1, -1).includes("$") &&
      !trimmed.includes("\n")) ||
    (trimmed.startsWith("\\(") &&
      trimmed.endsWith("\\)") &&
      trimmed.length >= 4)
  ) {
    let latex = "";
    if (trimmed.startsWith("$")) latex = trimmed.slice(1, -1).trim();
    else if (trimmed.startsWith("\\(")) latex = trimmed.slice(2, -2).trim();
    if (inlineMathType) {
      return [inlineMathType.create({ latex })];
    }
  }

  // Regex to match block equations across multi-line text:
  const blockMathRegex =
    /(?:\$\$\$\$([\s\S]+?)\$\$\$\$|\\\[([\s\S]+?)\\\]|(?:\n|^)\s*(?<!\$)\$\$(?!\$)([\s\S]+?)(?<!\$)\$\$(?!\$)\s*(?:\n|$))/g;

  const nodes: ProseMirrorNode[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  const parseInlineInParagraph = (
    paragraphText: string,
  ): ProseMirrorNode | null => {
    if (!paragraphText.trim()) return null;
    const inlineNodes: ProseMirrorNode[] = [];
    const inlineRegex =
      /(?:(?<!\$)\$\$(?!\$)([^$\n]+?)(?<!\$)\$\$(?!\$)|(?<!\$)\$(?!\$)([^$\n]+?)(?<!\\)\$(?!\$)|\\\(([\s\S]+?)\\\))/g;
    let inlineLastIndex = 0;
    let inlineMatch: RegExpExecArray | null;

    while ((inlineMatch = inlineRegex.exec(paragraphText)) !== null) {
      const before = paragraphText.slice(inlineLastIndex, inlineMatch.index);
      if (before) {
        inlineNodes.push(schema.text(before));
      }
      const latex = (
        inlineMatch[1] ??
        inlineMatch[2] ??
        inlineMatch[3] ??
        ""
      ).trim();
      if (latex && inlineMathType) {
        inlineNodes.push(inlineMathType.create({ latex }));
      } else if (inlineMatch[0]) {
        inlineNodes.push(schema.text(inlineMatch[0]));
      }
      inlineLastIndex = inlineMatch.index + inlineMatch[0].length;
    }

    const remaining = paragraphText.slice(inlineLastIndex);
    if (remaining) {
      inlineNodes.push(schema.text(remaining));
    }

    if (inlineNodes.length === 0) return null;
    return paragraphType ? paragraphType.create(null, inlineNodes) : null;
  };

  const processTextSegment = (segment: string) => {
    const paragraphs = segment.split(/\n\n+/);
    for (const pText of paragraphs) {
      const lines = pText.split("\n");
      const combined = lines.join(" ");
      const pNode = parseInlineInParagraph(combined);
      if (pNode) {
        nodes.push(pNode);
      }
    }
  };

  while ((match = blockMathRegex.exec(normalized)) !== null) {
    const segmentBefore = normalized.slice(lastIndex, match.index);
    if (segmentBefore) {
      processTextSegment(segmentBefore);
    }
    const latex = (match[1] ?? match[2] ?? match[3] ?? "").trim();
    if (latex && blockMathType) {
      nodes.push(blockMathType.create({ latex }));
    }
    lastIndex = match.index + match[0].length;
  }

  const segmentAfter = normalized.slice(lastIndex);
  if (segmentAfter) {
    processTextSegment(segmentAfter);
  }

  return nodes;
}

type Collaborator = { color: string; name: string };

export function ArticleEditor({
  blocks,
  canCommit,
  canEdit,
  chapterTags,
  collaborator,
  draftRevision,
  immersive,
  onFlush,
  onInsertArtifact,
  onInsertZotero,
  onOpenCommit,
  onOutlineChange,
  onReviewBlock,
  onReviewChapter,
  onToggleImmersive,
  projectId,
  provider,
}: Readonly<{
  blocks: ArticleBlock[];
  canCommit?: boolean;
  canEdit: boolean;
  chapterTags: ArticleChapterTag[];
  collaborator: Collaborator;
  draftRevision?: number;
  immersive?: boolean;
  onFlush: () => void;
  onInsertArtifact: (
    artifact: ArticleArtifactDrop,
  ) => Promise<{ reference_id: string }>;
  onInsertZotero: (
    item: ArticleZoteroDrop,
  ) => Promise<{ reference_id: string }>;
  onOpenCommit?: () => void;
  onOutlineChange: (items: ArticleOutlineItem[]) => void;
  onReviewBlock: (blockId: string, contentFingerprint: string) => Promise<void>;
  onReviewChapter: (chapterTagId: string) => Promise<void>;
  onToggleImmersive?: () => void;
  projectId: string;
  provider: HocuspocusProvider;
}>) {
  const [dropError, setDropError] = useState<string>();
  const [renderTheme, setRenderTheme] = useState<ArticleRenderTheme>("md");
  const [hoverMenu, setHoverMenu] = useState<{
    kind: ArticleNodeMenuKind;
    left: number;
    placement: "above" | "below";
    pos: number;
    top: number;
  }>();
  const [menuOpen, setMenuOpen] = useState(false);
  const [captionDraft, setCaptionDraft] = useState("");
  const [altDraft, setAltDraft] = useState("");
  const [replaceDraft, setReplaceDraft] = useState("");
  const [tagPositions, setTagPositions] = useState<Record<string, number>>({});
  const [blockMenuAnchor, setBlockMenuAnchor] = useState<{
    left: number;
    pos: number;
    top: number;
  }>();
  const [blockDraggingPos, setBlockDraggingPos] = useState<number>();
  const [dropIndicator, setDropIndicator] = useState<ArticleDropIndicator>();
  const [inlineDropIndicator, setInlineDropIndicator] = useState<{
    height: number;
    left: number;
    pos: number;
    top: number;
  }>();
  const inlineDropIndicatorRef = useRef<
    | {
        height: number;
        left: number;
        pos: number;
        top: number;
      }
    | undefined
  >(undefined);
  const [tableEdgeHandle, setTableEdgeHandle] =
    useState<ArticleTableEdgeHandle>();
  const [tableEdgeMenuOpen, setTableEdgeMenuOpen] = useState(false);
  const [tableDragIndicator, setTableDragIndicator] =
    useState<TableDragIndicator>();
  const [mathEditorTarget, setMathEditorTarget] = useState<{
    kind: ArticleMathKind;
    pos: number;
  }>();
  const [mathDraft, setMathDraft] = useState("");
  const dragHandlePos = useRef<number | null>(null);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const replacementImageInputRef = useRef<HTMLInputElement>(null);
  const replacementImageTargetRef = useRef<ImageReplacementTarget | undefined>(
    undefined,
  );
  const activeOutlineIdRef = useRef("");
  const outlineRef = useRef<ArticleOutlineItem[]>([]);
  const blockDraggingPosRef = useRef<number | null>(null);
  const blockDraggingRangeRef = useRef<{
    from: number;
    to: number;
  } | null>(null);
  const blockPointerDragActiveRef = useRef(false);
  const blockPointerDragCleanupRef = useRef<() => void>(() => undefined);
  const dropIndicatorRef = useRef<ArticleDropIndicator | undefined>(undefined);
  const marqueeRef = useRef<HTMLDivElement>(null);
  const blockMenuOpenRef = useRef(false);
  const editorSurfaceRef = useRef<HTMLDivElement>(null);
  const tableDragSessionRef = useRef<TableDragSession | undefined>(undefined);
  const tableDragCleanupRef = useRef<() => void>(() => undefined);
  const suppressTableHandleClickRef = useRef(false);
  const autoScrollFrameRef = useRef<number | null>(null);
  const lastDragClientYRef = useRef<number | null>(null);
  const lastDragCoordsRef = useRef<{ clientX: number; clientY: number } | null>(
    null,
  );
  const lastDragOptionsRef = useRef<{
    autoScroll: boolean;
    dropEffect: "copy" | "move";
  } | null>(null);
  const updateDropIndicatorRef = useRef<
    | ((
        event: globalThis.DragEvent,
        options: { autoScroll: boolean; dropEffect: "copy" | "move" },
      ) => boolean)
    | null
  >(null);
  const openMathEditor = useCallback(
    (kind: ArticleMathKind, node: ProseMirrorNode, pos: number) => {
      setMathDraft(String(node.attrs.latex ?? ""));
      setMathEditorTarget({ kind, pos });
    },
    [],
  );
  const renderingSetting = useQuery({
    queryFn: () =>
      optionalRequest<{ values: Record<string, unknown> }>(
        apiClient,
        `/projects/${encodeURIComponent(projectId)}/settings/article.rendering`,
      ),
    queryKey: ["article-rendering-setting", projectId],
    retry: false,
  });
  const editor = useEditor(
    {
      editable: canEdit,
      extensions: [
        StarterKit.configure({ dropcursor: false, undoRedo: false }),
        ...createArticleNodes(projectId),
        Mathematics.configure({
          blockOptions: {
            onClick: (node, pos) => openMathEditor("block", node, pos),
          },
          inlineOptions: {
            onClick: (node, pos) => openMathEditor("inline", node, pos),
          },
          katexOptions: { throwOnError: false },
        }),
        ArticleMathShortcuts.configure({
          onCreate: (kind, pos) => {
            setMathDraft("x");
            setMathEditorTarget({ kind, pos });
          },
        }),
        NodeRange.configure({ depth: 0, key: null }),
        TableKit.configure({ table: false }),
        SlashCommand,
        UniqueID.configure({
          types: [
            "paragraph",
            "heading",
            "blockquote",
            "codeBlock",
            "mathBlock",
            "blockMath",
            "table",
            "articleImage",
            "articleImageGroup",
            "artifactReference",
            "experimentResult",
            "modelReference",
          ],
        }),
        Collaboration.configure({
          document: provider.document,
          field: "default",
        }),
        CollaborationCaret.configure({ provider, user: collaborator }),
      ],
      editorProps: {
        transformPastedHTML: (html) => transformMathInHtml(html),
        handlePaste: (view, event) => {
          if (!view.editable) return false;

          // 1. Check for artifact in clipboard (from Model Reference or Artifact copy)
          const jsonText =
            event.clipboardData?.getData(
              "application/vnd.mmdash.artifact+json",
            ) || event.clipboardData?.getData("application/json");
          const htmlText = event.clipboardData?.getData("text/html");
          const text = event.clipboardData?.getData("text/plain");

          let artifactPayload: ArticleArtifactDrop | undefined;

          if (jsonText) {
            try {
              const parsed = JSON.parse(jsonText);
              if (
                parsed &&
                typeof parsed.artifactId === "string" &&
                typeof parsed.versionId === "string"
              ) {
                artifactPayload = {
                  artifactId: parsed.artifactId,
                  filename: String(parsed.filename || "image.png"),
                  mimeType: String(parsed.mimeType || "image/png"),
                  title: String(parsed.title || "模型图片"),
                  versionId: parsed.versionId,
                };
              }
            } catch {
              // ignore json parse error
            }
          }

          if (!artifactPayload && htmlText) {
            const match = htmlText.match(
              /data-artifact-id="([^"]+)"[\s\S]*?data-version-id="([^"]+)"/i,
            );
            if (match) {
              const artifactId = match[1];
              const versionId = match[2];
              const titleMatch = htmlText.match(/data-title="([^"]+)"/i);
              const mimeMatch = htmlText.match(/data-mime-type="([^"]+)"/i);
              const filenameMatch = htmlText.match(/data-filename="([^"]+)"/i);
              artifactPayload = {
                artifactId,
                filename: filenameMatch ? filenameMatch[1] : "image.png",
                mimeType: mimeMatch ? mimeMatch[1] : "image/png",
                title: titleMatch ? titleMatch[1] : "模型图片",
                versionId,
              };
            }
          }

          if (!artifactPayload && text) {
            const match = text
              .trim()
              .match(
                /^!\[(.*?)\]\(artifact:\/\/([0-9a-fA-F-]+)\?version=([0-9a-fA-F-]+)\)$/,
              );
            if (match) {
              artifactPayload = {
                artifactId: match[2],
                filename: "image.png",
                mimeType: "image/png",
                title: match[1] || "模型图片",
                versionId: match[3],
              };
            }
          }

          if (artifactPayload) {
            event.preventDefault();
            void (async () => {
              try {
                const reference = await onInsertArtifact(artifactPayload!);
                const node = view.state.schema.nodes.artifactReference.create({
                  alt: artifactPayload!.title,
                  align: "center",
                  artifactId: artifactPayload!.artifactId,
                  mimeType: artifactPayload!.mimeType,
                  objectId: artifactPayload!.artifactId,
                  referenceId: reference.reference_id,
                  title: artifactPayload!.title,
                  versionId: artifactPayload!.versionId,
                  width: 100,
                });
                const { $from } = view.state.selection;
                if (
                  $from.parent.isTextblock &&
                  $from.parent.content.size === 0
                ) {
                  view.dispatch(
                    view.state.tr
                      .replaceWith($from.before(), $from.after(), node)
                      .scrollIntoView(),
                  );
                } else {
                  view.dispatch(
                    view.state.tr.replaceSelectionWith(node).scrollIntoView(),
                  );
                }
              } catch (err) {
                console.error("Failed to paste artifact reference", err);
              }
            })();
            return true;
          }

          // 2. Math formula pasting
          if (!text) return false;
          const hasMath =
            text.includes("$") || text.includes("\\[") || text.includes("\\(");
          if (!hasMath) return false;

          const nodes = parseTextWithMath(text, view.state.schema);
          if (nodes.length === 0) return false;

          event.preventDefault();

          if (nodes.length === 1 && nodes[0].type.name === "inlineMath") {
            view.dispatch(
              view.state.tr.replaceSelectionWith(nodes[0]).scrollIntoView(),
            );
            return true;
          }

          if (nodes.length === 1 && nodes[0].type.name === "blockMath") {
            const { $from } = view.state.selection;
            if ($from.parent.isTextblock && $from.parent.content.size === 0) {
              view.dispatch(
                view.state.tr
                  .replaceWith($from.before(), $from.after(), nodes[0])
                  .scrollIntoView(),
              );
              return true;
            }
            view.dispatch(
              view.state.tr.replaceSelectionWith(nodes[0]).scrollIntoView(),
            );
            return true;
          }

          const fragment = Fragment.fromArray(nodes);
          const slice = new Slice(fragment, 0, 0);
          view.dispatch(view.state.tr.replaceSelection(slice).scrollIntoView());
          return true;
        },
        handleKeyDown: (view, event) => {
          if (
            event.key === "Escape" &&
            (blockMenuOpenRef.current ||
              view.state.selection instanceof NodeSelection ||
              isNodeRangeSelection(view.state.selection))
          ) {
            blockMenuOpenRef.current = false;
            setBlockMenuAnchor(undefined);
            if (
              view.state.selection instanceof NodeSelection ||
              isNodeRangeSelection(view.state.selection)
            ) {
              const position = view.state.selection.from;
              view.dispatch(
                view.state.tr
                  .setSelection(
                    TextSelection.near(
                      view.state.doc.resolve(
                        Math.min(position + 1, view.state.doc.content.size),
                      ),
                      1,
                    ),
                  )
                  .scrollIntoView(),
              );
              view.focus();
            }
            return true;
          }
          if (
            view.editable &&
            (event.key === "Backspace" || event.key === "Delete") &&
            (view.state.selection instanceof NodeSelection ||
              isNodeRangeSelection(view.state.selection))
          ) {
            return deleteSelectedArticleBlock(view);
          }
          return false;
        },
      },
      immediatelyRender: false,
    },
    [onInsertArtifact, openMathEditor, projectId, provider],
  );

  useEffect(() => {
    if (!editor) return;
    editor.commands.updateUser(collaborator);
  }, [collaborator, editor]);

  function blockAt(position: number) {
    if (!editor) return;
    const node = editor.state.doc.nodeAt(position);
    if (!node || editor.state.doc.resolve(position).depth !== 0) return;
    return node;
  }

  function clearDropIndicator() {
    dropIndicatorRef.current = undefined;
    setDropIndicator(undefined);
    inlineDropIndicatorRef.current = undefined;
    setInlineDropIndicator(undefined);
  }

  const stopAutoScroll = useCallback(() => {
    if (autoScrollFrameRef.current !== null) {
      cancelAnimationFrame(autoScrollFrameRef.current);
      autoScrollFrameRef.current = null;
    }
    lastDragClientYRef.current = null;
  }, []);

  const startAutoScroll = useCallback(
    (clientY: number) => {
      lastDragClientYRef.current = clientY;

      if (autoScrollFrameRef.current !== null) return;

      const scrollLoop = () => {
        const surface = editorSurfaceRef.current;
        if (!surface || lastDragClientYRef.current === null) {
          stopAutoScroll();
          return;
        }

        const rect = surface.getBoundingClientRect();
        const y = lastDragClientYRef.current;
        const margin = 72;
        const maxSpeed = 15;
        let speed = 0;
        if (y < rect.top + margin) {
          const ratio = Math.max(0, (rect.top + margin - y) / margin);
          speed = -ratio * maxSpeed;
        } else if (y > rect.bottom - margin) {
          const ratio = Math.max(0, (y - (rect.bottom - margin)) / margin);
          speed = ratio * maxSpeed;
        }

        if (speed !== 0) {
          const prevScrollTop = surface.scrollTop;
          surface.scrollTop += speed;
          if (
            surface.scrollTop !== prevScrollTop &&
            lastDragCoordsRef.current &&
            lastDragOptionsRef.current
          ) {
            const dummyEvent = {
              clientX: lastDragCoordsRef.current.clientX,
              clientY: lastDragCoordsRef.current.clientY,
              dataTransfer: {
                dropEffect: lastDragOptionsRef.current.dropEffect,
                types: ["Files"],
              },
              preventDefault: () => undefined,
            } as unknown as globalThis.DragEvent;
            updateDropIndicator(dummyEvent, lastDragOptionsRef.current);
          }
          autoScrollFrameRef.current = requestAnimationFrame(scrollLoop);
        } else {
          stopAutoScroll();
        }
      };

      autoScrollFrameRef.current = requestAnimationFrame(scrollLoop);
    },
    [stopAutoScroll],
  );

  function updateDropIndicator(
    event: DragEvent<HTMLDivElement> | globalThis.DragEvent,
    options: { autoScroll: boolean; dropEffect: "copy" | "move" },
  ) {
    const surface = editorSurfaceRef.current;
    if (!surface || !editor) return false;
    event.preventDefault();
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = options.dropEffect;
    }

    lastDragCoordsRef.current = {
      clientX: event.clientX,
      clientY: event.clientY,
    };
    lastDragOptionsRef.current = options;

    const root = surface.getBoundingClientRect();
    if (options.autoScroll) {
      startAutoScroll(event.clientY);
    }

    const coordinates = editor.view.posAtCoords({
      left: event.clientX,
      top: event.clientY,
    });
    if (!coordinates) return true;
    let blockPosition: number | undefined;
    let blockNode: ReturnType<typeof blockAt>;
    let offset = 0;
    editor.state.doc.forEach((node) => {
      if (
        blockPosition === undefined &&
        coordinates.pos >= offset &&
        coordinates.pos <= offset + node.nodeSize
      ) {
        blockPosition = offset;
        blockNode = node;
      }
      offset += node.nodeSize;
    });
    if (blockPosition === undefined || !blockNode) return true;

    const isImageOrArtifact =
      event.dataTransfer &&
      (event.dataTransfer.types.includes(articleArtifactMime) ||
        event.dataTransfer.types.includes("Files") ||
        event.dataTransfer.types.includes(
          "application/vnd.mmdash.image-group-item",
        ));

    const targetEl =
      typeof document !== "undefined"
        ? document.elementFromPoint(event.clientX, event.clientY)
        : null;

    const isOverImageContainer = Boolean(
      targetEl?.closest?.(
        "[data-article-image-group], figure[data-article-image], [data-article-artifact-image]",
      ) ||
      (blockNode &&
        (blockNode.type.name === "articleImageGroup" ||
          blockNode.type.name === "articleImage" ||
          (blockNode.type.name === "artifactReference" &&
            String(blockNode.attrs.mimeType ?? "").startsWith("image/")))),
    );

    if (isImageOrArtifact && isOverImageContainer) {
      clearDropIndicator();
      return true;
    }

    const dom = editor.view.nodeDOM(blockPosition);
    if (!(dom instanceof HTMLElement)) return true;
    const rect = dom.getBoundingClientRect();
    let before = event.clientY < rect.top + rect.height / 2;

    const draggingPosition = blockDraggingPosRef.current;
    if (draggingPosition !== null && draggingPosition !== undefined) {
      const range = blockDraggingRangeRef.current;
      let rangeFrom = draggingPosition;
      let rangeTo = draggingPosition;
      if (range) {
        rangeFrom = range.from;
        rangeTo = range.to;
      } else {
        const draggedNode = editor.state.doc.nodeAt(draggingPosition);
        if (draggedNode) {
          rangeTo = draggingPosition + draggedNode.nodeSize;
        }
      }

      if (blockPosition === rangeTo) {
        before = false;
      } else if (blockPosition + blockNode.nodeSize === rangeFrom) {
        before = true;
      }
    }

    const nextIndicator = {
      label: before ? "放在此块之前" : "放在此块之后",
      position: dropTargetPosition(blockPosition, blockNode.nodeSize, before),
      top: dropIndicatorOffset(
        before ? rect.top : rect.bottom,
        root.top,
        surface.scrollTop,
      ),
    } satisfies ArticleDropIndicator;
    dropIndicatorRef.current = nextIndicator;
    setDropIndicator(nextIndicator);
    return true;
  }

  updateDropIndicatorRef.current = updateDropIndicator;

  useEffect(() => {
    const handleDragWheel = (event: WheelEvent) => {
      if (blockDraggingPosRef.current === null) return;
      const surface = editorSurfaceRef.current;
      if (!surface) return;
      const delta = wheelScrollDelta(
        event.deltaY,
        event.deltaMode,
        surface.clientHeight,
      );
      if (delta === 0) return;

      event.preventDefault();
      event.stopPropagation();
      const previousScrollTop = surface.scrollTop;
      surface.scrollTop += delta;
      if (
        surface.scrollTop !== previousScrollTop &&
        lastDragCoordsRef.current &&
        lastDragOptionsRef.current
      ) {
        const syntheticDragEvent = {
          clientX: lastDragCoordsRef.current.clientX,
          clientY: lastDragCoordsRef.current.clientY,
          dataTransfer: {
            dropEffect: lastDragOptionsRef.current.dropEffect,
            types: ["Files"],
          },
          preventDefault: () => undefined,
        } as unknown as globalThis.DragEvent;
        updateDropIndicatorRef.current?.(
          syntheticDragEvent,
          lastDragOptionsRef.current,
        );
      }
    };

    window.addEventListener("wheel", handleDragWheel, {
      capture: true,
      passive: false,
    });
    return () => {
      window.removeEventListener("wheel", handleDragWheel, { capture: true });
    };
  }, []);

  useEffect(() => {
    return () => blockPointerDragCleanupRef.current();
  }, []);

  useEffect(() => {
    const readTheme = () => void renderingSetting.refetch();
    const value = renderingSetting.data?.values.theme;
    setRenderTheme(value === "latex" ? "latex" : "md");
    window.addEventListener(ARTICLE_RENDER_THEME_EVENT, readTheme);
    return () => {
      window.removeEventListener(ARTICLE_RENDER_THEME_EVENT, readTheme);
    };
  }, [renderingSetting.data, renderingSetting.refetch]);

  useEffect(() => {
    editor?.setEditable(canEdit);
  }, [canEdit, editor]);

  useEffect(() => {
    if (!editor || !canEdit) return;
    const surface = editorSurfaceRef.current;
    if (!surface) return;
    type SelectableBlock = {
      pos: number;
      rectangle: EditorRectangle;
      size: number;
    };
    let session:
      | {
          active: boolean;
          blocks?: SelectableBlock[];
          latest?: { clientX: number; clientY: number };
          pointerId: number;
          start: { x: number; y: number };
        }
      | undefined;
    let frame = 0;

    const contentPoint = (
      clientX: number,
      clientY: number,
      surfaceRect = surface.getBoundingClientRect(),
    ) => {
      return {
        x: clientX - surfaceRect.left + surface.scrollLeft,
        y: clientY - surfaceRect.top + surface.scrollTop,
      };
    };
    const measureBlocks = (surfaceRect: DOMRect): SelectableBlock[] => {
      const measured: SelectableBlock[] = [];
      for (const child of Array.from(editor.view.dom.children)) {
        if (!(child instanceof HTMLElement)) continue;
        const childRect = child.getBoundingClientRect();
        try {
          const pos = editor.view.posAtDOM(child, 0);
          const node = editor.state.doc.nodeAt(pos);
          if (node) {
            measured.push({
              pos,
              rectangle: {
                bottom: childRect.bottom - surfaceRect.top + surface.scrollTop,
                left: childRect.left - surfaceRect.left + surface.scrollLeft,
                right: childRect.right - surfaceRect.left + surface.scrollLeft,
                top: childRect.top - surfaceRect.top + surface.scrollTop,
              },
              size: node.nodeSize,
            });
          }
        } catch {
          // Ignore non-document decoration nodes.
        }
      }
      return measured;
    };
    const updateSelection = (
      selectionRectangle: EditorRectangle,
      blocks: SelectableBlock[],
    ) => {
      const matches = blocks.filter(({ rectangle }) =>
        rectanglesIntersect(selectionRectangle, rectangle),
      );
      if (!matches.length) return;
      const first = matches[0]!;
      const last = matches[matches.length - 1]!;
      const nextSelection = NodeRangeSelection.create(
        editor.state.doc,
        first.pos,
        last.pos + last.size,
        0,
      );
      if (!editor.state.selection.eq(nextSelection)) {
        editor.view.dispatch(editor.state.tr.setSelection(nextSelection));
      }
    };
    const drawMarquee = (rectangle?: EditorRectangle) => {
      const marquee = marqueeRef.current;
      if (!marquee) return;
      if (!rectangle) {
        marquee.hidden = true;
        return;
      }
      marquee.hidden = false;
      marquee.style.cssText = `width:${rectangle.right - rectangle.left}px;height:${rectangle.bottom - rectangle.top}px;transform:translate3d(${rectangle.left}px,${rectangle.top}px,0)`;
    };
    const clearBlockSelection = () => {
      const { selection } = editor.state;
      if (
        !(selection instanceof NodeSelection) &&
        !isNodeRangeSelection(selection)
      ) {
        return;
      }
      const position = Math.min(
        selection.from + 1,
        editor.state.doc.content.size,
      );
      editor.view.dispatch(
        editor.state.tr.setSelection(
          TextSelection.near(editor.state.doc.resolve(position), 1),
        ),
      );
      editor.view.focus();
    };
    const placeCaretAtBlankPoint = (clientX: number, clientY: number) => {
      const target = document.elementFromPoint(clientX, clientY);
      if (!(target instanceof Element) || !editor.view.dom.contains(target))
        return false;
      const textBlockSelector =
        "p, h1, h2, h3, h4, h5, h6, pre, [data-type='codeBlock']";
      let textBlock = target.closest(textBlockSelector);
      if (!textBlock || !editor.view.dom.contains(textBlock)) {
        let topLevel: Element | null = target;
        while (
          topLevel?.parentElement &&
          topLevel.parentElement !== editor.view.dom
        ) {
          topLevel = topLevel.parentElement;
        }
        const candidates = topLevel
          ? Array.from(topLevel.querySelectorAll(textBlockSelector))
          : [];
        textBlock =
          candidates.find((candidate) => {
            const rect = candidate.getBoundingClientRect();
            return clientY >= rect.top && clientY <= rect.bottom;
          }) ??
          candidates.at(-1) ??
          null;
      }
      if (!textBlock) return false;
      try {
        const endPosition = editor.view.posAtDOM(
          textBlock,
          textBlock.childNodes.length,
        );
        editor.view.dispatch(
          editor.state.tr.setSelection(
            TextSelection.near(editor.state.doc.resolve(endPosition), -1),
          ),
        );
        editor.view.focus();
        return true;
      } catch {
        // The target may disappear after a concurrent collaboration update.
        return false;
      }
    };
    const processPointerFrame = () => {
      frame = 0;
      if (!session?.latest) return;
      let surfaceRect = surface.getBoundingClientRect();
      let current = contentPoint(
        session.latest.clientX,
        session.latest.clientY,
        surfaceRect,
      );
      if (
        !session.active &&
        Math.hypot(current.x - session.start.x, current.y - session.start.y) < 4
      ) {
        return;
      }
      if (!session.active) {
        session.active = true;
        session.blocks = measureBlocks(surfaceRect);
      }
      const edge = 48;
      let scrolled = false;
      if (session.latest.clientY < surfaceRect.top + edge) {
        const previous = surface.scrollTop;
        surface.scrollTop -= 12;
        scrolled = surface.scrollTop !== previous;
      } else if (session.latest.clientY > surfaceRect.bottom - edge) {
        const previous = surface.scrollTop;
        surface.scrollTop += 12;
        scrolled = surface.scrollTop !== previous;
      }
      if (scrolled) {
        surfaceRect = surface.getBoundingClientRect();
        current = contentPoint(
          session.latest.clientX,
          session.latest.clientY,
          surfaceRect,
        );
      }
      const rectangle = rectangleFromPoints(session.start, current);
      drawMarquee(rectangle);
      updateSelection(rectangle, session.blocks ?? []);
      if (scrolled && !frame)
        frame = requestAnimationFrame(processPointerFrame);
    };
    const schedulePointerFrame = () => {
      if (!frame) frame = requestAnimationFrame(processPointerFrame);
    };
    const pointerDown = (event: PointerEvent) => {
      if (event.button !== 0 || !event.isPrimary) return;
      const target = event.target;
      if (!(target instanceof Element)) return;
      if (
        target.closest(
          "button, input, textarea, select, a, [role='menu'], .drag-handle, [data-article-table-controls], [data-type='inline-math'], [data-type='block-math'], table, figure",
        ) ||
        pointIsOverEditableText(editor.view.dom, event.clientX, event.clientY)
      ) {
        return;
      }
      session = {
        active: false,
        pointerId: event.pointerId,
        start: contentPoint(event.clientX, event.clientY),
      };
      event.preventDefault();
    };
    const pointerMove = (event: PointerEvent) => {
      if (!session || event.pointerId !== session.pointerId) return;
      session.latest = { clientX: event.clientX, clientY: event.clientY };
      event.preventDefault();
      schedulePointerFrame();
    };
    const pointerUp = (event: PointerEvent) => {
      if (!session || event.pointerId !== session.pointerId) return;
      if (frame) {
        cancelAnimationFrame(frame);
        frame = 0;
        processPointerFrame();
      }
      const wasActive = session.active;
      session = undefined;
      cancelAnimationFrame(frame);
      frame = 0;
      drawMarquee();
      if (!wasActive && !placeCaretAtBlankPoint(event.clientX, event.clientY)) {
        clearBlockSelection();
      }
    };
    const scroll = () => {
      if (session?.active) schedulePointerFrame();
    };
    surface.addEventListener("pointerdown", pointerDown);
    surface.addEventListener("scroll", scroll, { passive: true });
    window.addEventListener("pointermove", pointerMove, { passive: false });
    window.addEventListener("pointerup", pointerUp);
    window.addEventListener("pointercancel", pointerUp);
    return () => {
      session = undefined;
      cancelAnimationFrame(frame);
      drawMarquee();
      surface.removeEventListener("pointerdown", pointerDown);
      surface.removeEventListener("scroll", scroll);
      window.removeEventListener("pointermove", pointerMove);
      window.removeEventListener("pointerup", pointerUp);
      window.removeEventListener("pointercancel", pointerUp);
    };
  }, [canEdit, editor]);

  useEffect(
    () => () => {
      tableDragCleanupRef.current();
    },
    [],
  );

  useEffect(() => {
    if (
      !blockMenuAnchor &&
      !menuOpen &&
      !tableEdgeMenuOpen &&
      !mathEditorTarget
    )
      return;
    const closeOverlays = (event?: Event) => {
      const target = event?.target;
      if (
        target instanceof Element &&
        target.closest(
          "[data-article-block-menu], [data-article-node-menu], [data-article-table-controls], [data-article-math-editor], [data-type='inline-math'], [data-type='block-math'], .drag-handle",
        )
      ) {
        return;
      }
      blockMenuOpenRef.current = false;
      setBlockMenuAnchor(undefined);
      setMenuOpen(false);
      setTableEdgeMenuOpen(false);
      setTableEdgeHandle(undefined);
      setMathEditorTarget(undefined);
      if (event?.type === "scroll" || event?.type === "resize") {
        setHoverMenu(undefined);
      }
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeOverlays();
      editor?.view.focus();
    };
    document.addEventListener("pointerdown", closeOverlays, true);
    document.addEventListener("keydown", escape, true);
    window.addEventListener("resize", closeOverlays);
    const surface = editorSurfaceRef.current;
    surface?.addEventListener("scroll", closeOverlays, { passive: true });
    return () => {
      document.removeEventListener("pointerdown", closeOverlays, true);
      document.removeEventListener("keydown", escape, true);
      window.removeEventListener("resize", closeOverlays);
      surface?.removeEventListener("scroll", closeOverlays);
    };
  }, [blockMenuAnchor, editor, mathEditorTarget, menuOpen, tableEdgeMenuOpen]);

  useEffect(() => {
    if (!canEdit) return;
    const openImageUpload = () => imageInputRef.current?.click();
    window.addEventListener(openArticleImageUploadEvent, openImageUpload);
    return () =>
      window.removeEventListener(openArticleImageUploadEvent, openImageUpload);
  }, [canEdit]);

  useEffect(() => {
    if (!editor) return;
    const surface = editorSurfaceRef.current;
    const publishActive = () => {
      if (!surface) return;
      const surfaceTop = surface.getBoundingClientRect().top;
      let firstId = "";
      let activeId = "";
      editor.state.doc.descendants((node, pos) => {
        if (node.type.name !== "heading") return;
        const id = String(node.attrs.id ?? "");
        if (!id) return;
        if (!firstId) firstId = id;
        const dom = editor.view.nodeDOM(pos);
        if (
          dom instanceof HTMLElement &&
          dom.getBoundingClientRect().top - surfaceTop <= 96
        ) {
          activeId = id;
        }
      });
      activeId ||= firstId;
      if (activeId === activeOutlineIdRef.current) return;
      activeOutlineIdRef.current = activeId;
      window.dispatchEvent(
        new CustomEvent(articleOutlineActiveEvent, {
          detail: { id: activeId },
        }),
      );
    };
    const publishOutline = () => {
      const items: ArticleOutlineItem[] = [];
      editor.state.doc.descendants((node) => {
        if (node.type.name !== "heading") return;
        const id = String(node.attrs.id ?? "");
        if (id)
          items.push({
            id,
            level: Number(node.attrs.level ?? 1),
            text: node.textContent.trim() || "无标题章节",
          });
      });
      if (!sameArticleOutline(outlineRef.current, items)) {
        outlineRef.current = items;
        onOutlineChange(items);
      }
      publishActive();
    };
    const navigate = (event: Event) => {
      const id = (event as CustomEvent<{ id?: string }>).detail?.id;
      if (!id) return;
      let targetPosition: number | undefined;
      editor.state.doc.descendants((node, pos) => {
        if (String(node.attrs.id ?? "") === id) {
          targetPosition = pos;
          return false;
        }
      });
      if (targetPosition === undefined) return;
      const dom = editor.view.nodeDOM(targetPosition);
      if (dom instanceof HTMLElement) {
        dom.scrollIntoView({ behavior: "smooth", block: "start" });
        editor.commands.setTextSelection(targetPosition + 1);
        editor.commands.focus();
        activeOutlineIdRef.current = id;
        window.dispatchEvent(
          new CustomEvent(articleOutlineActiveEvent, { detail: { id } }),
        );
      }
    };
    publishOutline();
    editor.on("transaction", publishOutline);
    surface?.addEventListener("scroll", publishActive, {
      passive: true,
    });
    window.addEventListener(articleOutlineNavigateEvent, navigate);
    return () => {
      editor.off("transaction", publishOutline);
      surface?.removeEventListener("scroll", publishActive);
      window.removeEventListener(articleOutlineNavigateEvent, navigate);
    };
  }, [editor, onOutlineChange]);

  useEffect(() => {
    if (!editor || !canEdit) return;
    const insertReference = (event: Event) => {
      const detail = (event as CustomEvent<ArticleVersionedReferenceInsert>)
        .detail;
      if (
        !detail?.objectId ||
        !detail.referenceId ||
        !detail.versionId ||
        !detail.title
      )
        return;
      const type =
        detail.referenceType === "model_snapshot"
          ? "modelReference"
          : detail.referenceType === "experiment_result"
            ? "experimentResult"
            : "artifactReference";
      editor
        .chain()
        .focus()
        .insertContent({
          attrs: {
            experimentId:
              detail.referenceType === "experiment_result"
                ? detail.objectId
                : "",
            artifactId:
              detail.referenceType === "problem" ? detail.objectId : "",
            mimeType: detail.mimeType ?? "",
            objectId: detail.objectId,
            referenceId: detail.referenceId,
            title: detail.title,
            versionId: detail.versionId,
          },
          type,
        })
        .run();
    };
    window.addEventListener(articleInsertReferenceEvent, insertReference);
    return () => {
      window.removeEventListener(articleInsertReferenceEvent, insertReference);
    };
  }, [canEdit, editor]);

  useEffect(() => {
    if (!editor || !canEdit) return;

    const handleInsertArtifactIntoGroup = async (event: Event) => {
      const detail = (
        event as CustomEvent<{
          groupPos?: number;
          insertIndex: number;
          payload: ArticleArtifactDrop;
          standalonePos?: number;
        }>
      ).detail;
      if (!detail?.payload) return;

      try {
        const reference = await onInsertArtifact(detail.payload);
        const node = editor.schema.nodes.artifactReference.create({
          alt: detail.payload.title,
          align: "center",
          artifactId: detail.payload.artifactId,
          mimeType: detail.payload.mimeType,
          objectId: detail.payload.artifactId,
          referenceId: reference.reference_id,
          title: detail.payload.title,
          versionId: detail.payload.versionId,
          width: 100,
        });

        if (detail.groupPos !== undefined) {
          const inserted = insertArticleImageIntoGroup(
            editor,
            detail.groupPos,
            detail.insertIndex,
            node,
          );
          if (inserted) return;
        }

        if (detail.standalonePos !== undefined) {
          const standaloneNode = editor.state.doc.nodeAt(detail.standalonePos);
          if (standaloneNode && isArticleImageNode(standaloneNode)) {
            const groupType = editor.schema.nodes.articleImageGroup;
            if (groupType) {
              const children =
                detail.insertIndex > 0
                  ? [standaloneNode, node]
                  : [node, standaloneNode];
              const group = groupType.create(
                { caption: "", columns: 2 },
                Fragment.fromArray(children),
              );
              editor.view.dispatch(
                editor.state.tr.replaceWith(
                  detail.standalonePos,
                  detail.standalonePos + standaloneNode.nodeSize,
                  group,
                ),
              );
              editor.view.focus();
              return;
            }
          }
        }
      } catch (error) {
        setDropError(
          error instanceof Error ? error.message : "无法将图片插入组合",
        );
      }
    };

    const handleUploadImageIntoGroup = async (event: Event) => {
      const detail = (
        event as CustomEvent<{
          file: File;
          groupPos?: number;
          insertIndex: number;
          standalonePos?: number;
        }>
      ).detail;
      if (!detail?.file) return;

      try {
        setDropError(`正在上传图片 ${detail.file.name}…`);
        const articleFolder = await ensureArtifactRootFolder(
          projectId,
          "article",
        );
        const uploadTask = new MultipartUploadTask({
          file: detail.file,
          folderId: articleFolder.folder_id,
          kind: "attachment",
          name: detail.file.name,
          projectId,
          tags: ["article-image"],
        });
        const uploadDetail = await uploadTask.start();
        const placementMessage =
          uploadTask.getSnapshot().placementError?.message;
        const version = uploadDetail.current_version;
        if (!version || version.status !== "available") {
          throw new Error("图片上传完成，但不可变版本尚不可用");
        }
        const payload = {
          artifactId: uploadDetail.artifact.artifact_id,
          filename: version.filename,
          mimeType: version.mime_type,
          title: uploadDetail.artifact.name,
          versionId: version.version_id,
        } satisfies ArticleArtifactDrop;

        const reference = await onInsertArtifact(payload);
        const node = editor.schema.nodes.artifactReference.create({
          alt: payload.title,
          align: "center",
          artifactId: payload.artifactId,
          mimeType: payload.mimeType,
          objectId: payload.artifactId,
          referenceId: reference.reference_id,
          title: payload.title,
          versionId: payload.versionId,
          width: 100,
        });

        if (detail.groupPos !== undefined) {
          const inserted = insertArticleImageIntoGroup(
            editor,
            detail.groupPos,
            detail.insertIndex,
            node,
          );
          if (inserted) {
            setDropError(
              placementMessage ?? `图片 ${detail.file.name} 已加入图片组合`,
            );
            return;
          }
        }

        if (detail.standalonePos !== undefined) {
          const standaloneNode = editor.state.doc.nodeAt(detail.standalonePos);
          if (standaloneNode && isArticleImageNode(standaloneNode)) {
            const groupType = editor.schema.nodes.articleImageGroup;
            if (groupType) {
              const children =
                detail.insertIndex > 0
                  ? [standaloneNode, node]
                  : [node, standaloneNode];
              const group = groupType.create(
                { caption: "", columns: 2 },
                Fragment.fromArray(children),
              );
              editor.view.dispatch(
                editor.state.tr.replaceWith(
                  detail.standalonePos,
                  detail.standalonePos + standaloneNode.nodeSize,
                  group,
                ),
              );
              editor.view.focus();
              setDropError(
                placementMessage ?? `已创建图片组合并加入 ${detail.file.name}`,
              );
              return;
            }
          }
        }
      } catch (error) {
        setDropError(error instanceof Error ? error.message : "图片上传失败");
      }
    };

    window.addEventListener(
      articleInsertArtifactIntoGroupEvent,
      handleInsertArtifactIntoGroup,
    );
    window.addEventListener(
      articleUploadImageIntoGroupEvent,
      handleUploadImageIntoGroup,
    );
    return () => {
      window.removeEventListener(
        articleInsertArtifactIntoGroupEvent,
        handleInsertArtifactIntoGroup,
      );
      window.removeEventListener(
        articleUploadImageIntoGroupEvent,
        handleUploadImageIntoGroup,
      );
    };
  }, [canEdit, editor, onInsertArtifact, projectId]);

  useEffect(() => {
    if (!editor) return;
    const surface = editorSurfaceRef.current;
    if (!surface) return;
    let frame = 0;
    const measure = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const surfaceRect = surface.getBoundingClientRect();
        const next: Record<string, number> = {};
        editor.state.doc.descendants((node, pos) => {
          const id = String(node.attrs.id ?? "");
          if (!id) return;
          const dom = editor.view.nodeDOM(pos);
          if (dom instanceof HTMLElement) {
            next[id] =
              dom.getBoundingClientRect().top -
              surfaceRect.top +
              surface.scrollTop;
          }
        });
        setTagPositions((current) =>
          sameTagPositions(current, next) ? current : next,
        );
      });
    };
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(measure);
    observer?.observe(surface);
    observer?.observe(editor.view.dom);
    const tagLayer = surface.parentElement;
    const syncScrollOffset = () => {
      tagLayer?.style.setProperty(
        "--article-editor-scroll-offset",
        `${-surface.scrollTop}px`,
      );
    };
    surface.addEventListener("scroll", syncScrollOffset, { passive: true });
    editor.on("transaction", measure);
    syncScrollOffset();
    measure();
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      surface.removeEventListener("scroll", syncScrollOffset);
      tagLayer?.style.removeProperty("--article-editor-scroll-offset");
      editor.off("transaction", measure);
    };
  }, [blocks, editor]);

  useEffect(() => {
    const save = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        onFlush();
      }
    };
    window.addEventListener("keydown", save);
    return () => window.removeEventListener("keydown", save);
  }, [onFlush]);

  useEffect(() => {
    if (!editor || !canEdit) return;
    const migrate = ({ state }: { state: boolean }) => {
      if (!state) return;
      const replacements: {
        attrs: Record<string, unknown>;
        pos: number;
        type: "blockMath" | "inlineMath";
      }[] = [];
      editor.state.doc.descendants((node, pos) => {
        if (node.type.name === "mathBlock")
          replacements.push({ attrs: node.attrs, pos, type: "blockMath" });
        if (node.type.name === "mathInline")
          replacements.push({ attrs: node.attrs, pos, type: "inlineMath" });
      });
      if (!replacements.length) return;
      const transaction = editor.state.tr;
      for (const replacement of replacements.reverse()) {
        transaction.setNodeMarkup(
          replacement.pos,
          editor.schema.nodes[replacement.type],
          replacement.attrs,
        );
      }
      transaction.setMeta("addToHistory", false);
      editor.view.dispatch(transaction);
    };
    provider.on("synced", migrate);
    if (provider.synced) migrate({ state: true });
    return () => {
      provider.off("synced", migrate);
    };
  }, [canEdit, editor, provider]);

  useEffect(() => {
    if (!editor || !canEdit) return;
    const migrateLegacyCaptions = () => {
      migrateLegacyTableCaptions(editor);
    };
    provider.on("synced", migrateLegacyCaptions);
    if (provider.synced) migrateLegacyCaptions();
    return () => {
      provider.off("synced", migrateLegacyCaptions);
    };
  }, [canEdit, editor, provider]);

  const handleElementDragEnd = useCallback(() => {
    if (blockPointerDragActiveRef.current) return;
    blockDraggingPosRef.current = null;
    blockDraggingRangeRef.current = null;
    dropIndicatorRef.current = undefined;
    setBlockDraggingPos(undefined);
    setDropIndicator(undefined);
    stopAutoScroll();
  }, [stopAutoScroll]);

  const handleElementDragStart = useCallback(
    (event: globalThis.DragEvent) => {
      if (blockPointerDragActiveRef.current) {
        event.preventDefault();
        return;
      }
      if (!editor) return;
      const position = dragHandlePos.current;
      if (position === null || position === undefined) return;

      // Check if the drag handle block is within an existing multi-block
      // selection. If so, preserve the selection to drag all selected blocks.
      const selection = editor.state.selection;
      const isInMultiBlockSelection =
        isNodeRangeSelection(selection) &&
        position >= selection.from &&
        position < selection.to;

      if (isInMultiBlockSelection) {
        // Keep the existing multi-block selection
        blockDraggingRangeRef.current = {
          from: selection.from,
          to: selection.to,
        };
      } else {
        // Single block drag
        selectArticleBlock(editor, position, { scrollIntoView: false });
        blockDraggingRangeRef.current = null;
      }

      blockDraggingPosRef.current = position;
      dropIndicatorRef.current = undefined;
      setBlockDraggingPos(position);
      setDropIndicator(undefined);
      if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
    },
    [editor],
  );

  const handleDragNodeChange = useCallback(
    ({ node, pos }: { node: ProseMirrorNode | null; pos: number }) => {
      dragHandlePos.current = node ? pos : null;
      if (!node && !blockMenuOpenRef.current) setBlockMenuAnchor(undefined);
    },
    [],
  );

  if (!editor) return null;

  const closeMathEditor = () => {
    setMathEditorTarget(undefined);
    editor.view.focus();
  };

  const saveMathEditor = () => {
    if (!mathEditorTarget || !mathDraft.trim()) return;
    const node = editor.state.doc.nodeAt(mathEditorTarget.pos);
    const expectedName =
      mathEditorTarget.kind === "block" ? "blockMath" : "inlineMath";
    if (node?.type.name !== expectedName) {
      setMathEditorTarget(undefined);
      return;
    }
    if (mathEditorTarget.kind === "block") {
      editor.commands.updateBlockMath({
        latex: mathDraft.trim(),
        pos: mathEditorTarget.pos,
      });
    } else {
      editor.commands.updateInlineMath({
        latex: mathDraft.trim(),
        pos: mathEditorTarget.pos,
      });
    }
    closeMathEditor();
  };

  const deleteMathEditor = () => {
    if (!mathEditorTarget) return;
    if (mathEditorTarget.kind === "block") {
      editor.commands.deleteBlockMath({ pos: mathEditorTarget.pos });
    } else {
      editor.commands.deleteInlineMath({ pos: mathEditorTarget.pos });
    }
    closeMathEditor();
  };

  const insertMath = () => {
    const latex = window.prompt("输入 LaTeX 公式", "\\int_0^1 x^2\\,dx");
    if (latex) editor.chain().focus().insertBlockMath({ latex }).run();
  };

  const insertArtifactNode = async (
    payload: ArticleArtifactDrop,
    position = editor.state.selection.to,
  ) => {
    const reference = await onInsertArtifact(payload);
    editor
      .chain()
      .focus()
      .insertContentAt(position, {
        attrs: {
          alt: payload.title,
          align: "center",
          artifactId: payload.artifactId,
          mimeType: payload.mimeType,
          objectId: payload.artifactId,
          referenceId: reference.reference_id,
          title: payload.title,
          versionId: payload.versionId,
          width: 100,
        },
        type: "artifactReference",
      })
      .run();
  };

  const locateImageReplacement = (target: ImageReplacementTarget) => {
    let position: number | undefined;
    if (target.blockId) {
      editor.state.doc.descendants((node, pos) => {
        if (String(node.attrs.id ?? "") === target.blockId) {
          position = pos;
          return false;
        }
      });
    }
    position ??= target.fallbackPosition;
    const node = editor.state.doc.nodeAt(position);
    if (
      !node ||
      (node.type.name !== "articleImage" &&
        node.type.name !== "artifactReference")
    ) {
      return;
    }
    return { node, pos: position };
  };

  const replaceImageWithArtifact = async (
    payload: ArticleArtifactDrop,
    target: ImageReplacementTarget,
  ) => {
    if (!locateImageReplacement(target)) {
      throw new Error("原图片块已不存在，未执行替换");
    }
    const reference = await onInsertArtifact(payload);
    const located = locateImageReplacement(target);
    if (!located) throw new Error("上传期间原图片块已被删除，未执行替换");
    const replaced = replaceArticleImageWithArtifact(editor, located.pos, {
      artifactId: payload.artifactId,
      mimeType: payload.mimeType,
      referenceId: reference.reference_id,
      title: payload.title,
      versionId: payload.versionId,
    });
    if (!replaced) throw new Error("无法在原块位置替换图片");
  };

  const uploadImage = async (
    file: File,
    replacementTarget?: ImageReplacementTarget,
    insertionPosition?: number,
  ) => {
    if (!canEdit || !file.type.startsWith("image/")) {
      setDropError("请选择图片文件");
      return;
    }
    setDropError(`正在上传图片 ${file.name}…`);
    try {
      const articleFolder = await ensureArtifactRootFolder(
        projectId,
        "article",
      );
      const uploadTask = new MultipartUploadTask({
        file,
        folderId: articleFolder.folder_id,
        kind: "attachment",
        name: file.name,
        projectId,
        tags: ["article-image"],
      });
      const detail = await uploadTask.start();
      const placementMessage = uploadTask.getSnapshot().placementError?.message;
      const version = detail.current_version;
      if (!version || version.status !== "available") {
        throw new Error("图片上传完成，但不可变版本尚不可用");
      }
      const payload = {
        artifactId: detail.artifact.artifact_id,
        filename: version.filename,
        mimeType: version.mime_type,
        title: detail.artifact.name,
        versionId: version.version_id,
      } satisfies ArticleArtifactDrop;
      if (replacementTarget) {
        await replaceImageWithArtifact(payload, replacementTarget);
        setDropError(
          placementMessage ?? `图片 ${file.name} 已上传并替换原图片块`,
        );
      } else {
        await insertArtifactNode(payload, insertionPosition);
        setDropError(placementMessage ?? `图片 ${file.name} 已上传并插入`);
      }
    } catch (error) {
      setDropError(error instanceof Error ? error.message : "图片上传失败");
    }
  };

  const selectImage = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (file) void uploadImage(file);
  };

  const selectReplacementImage = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    const target = replacementImageTargetRef.current;
    event.target.value = "";
    replacementImageTargetRef.current = undefined;
    if (file && target) void uploadImage(file, target);
  };

  const pasteImage = (event: ClipboardEvent<HTMLDivElement>) => {
    const file = Array.from(event.clipboardData.files).find((item) =>
      item.type.startsWith("image/"),
    );
    if (!file) return;
    event.preventDefault();
    void uploadImage(file);
  };

  const dropArtifact = async (
    event: DragEvent<HTMLDivElement>,
    insertionPosition?: number,
  ) => {
    const isZotero = event.dataTransfer.types.includes(articleZoteroMime);
    const isArtifact = event.dataTransfer.types.includes(articleArtifactMime);
    if (!canEdit || (!isZotero && !isArtifact)) return;
    event.preventDefault();
    setDropError(undefined);
    try {
      if (isZotero) {
        const payload = parseArticleZoteroDrop(
          event.dataTransfer.getData(articleZoteroMime),
        );
        const reference = await onInsertZotero(payload);
        const attrs = {
          citationKey: payload.citationKey,
          itemKey: payload.itemKey,
          referenceId: reference.reference_id,
          title: payload.title,
          version: payload.version,
        };
        const target =
          insertionPosition === undefined
            ? editor.view.posAtCoords({
                left: event.clientX,
                top: event.clientY,
              })
            : undefined;
        const targetPos =
          insertionPosition ?? target?.pos ?? editor.state.selection.to;
        if (editor.schema.nodes.zoteroCitation) {
          editor
            .chain()
            .focus()
            .insertContentAt(targetPos, { attrs, type: "zoteroCitation" })
            .run();
        } else {
          editor
            .chain()
            .focus()
            .insertContentAt(targetPos, `[@${payload.citationKey}]`)
            .run();
        }
        return;
      }
      const payload = parseArticleArtifactDrop(
        event.dataTransfer.getData(articleArtifactMime),
      );
      const target =
        insertionPosition === undefined
          ? editor.view.posAtCoords({
              left: event.clientX,
              top: event.clientY,
            })
          : undefined;
      await insertArtifactNode(
        payload,
        insertionPosition ?? target?.pos ?? editor.state.selection.to,
      );
    } catch (error) {
      setDropError(
        error instanceof Error ? error.message : "Artifact 插入失败",
      );
    }
  };

  const insertTableCaption = () => {
    const table = findTableAtSelection();
    if (!table) {
      setDropError("请先把光标放在表格中，再编辑表注。");
      return;
    }
    openMenuForNode("table", table.pos);
  };

  const insertParagraphBeforeCurrent = () => {
    const position = dragHandlePos.current;
    if (position === null || position === undefined) return;
    editor
      .chain()
      .focus()
      .insertContentAt(position, { type: "paragraph" })
      .run();
    editor.commands.setTextSelection(position + 1);
    editor.view.focus();
  };

  const openBlockMenu = () => {
    const position = dragHandlePos.current;
    if (position === null || position === undefined) return;
    const node = blockAt(position);
    if (!node) return;
    const dom = editor.view.nodeDOM(position);
    const rect =
      dom instanceof HTMLElement ? dom.getBoundingClientRect() : null;
    if (!rect) return;
    blockMenuOpenRef.current = true;
    setBlockMenuAnchor({
      left: Math.max(8, Math.min(rect.left, window.innerWidth - 296)),
      pos: position,
      top: Math.max(8, Math.min(rect.top + 30, window.innerHeight - 536)),
    });
    selectArticleBlock(editor, position, { scrollIntoView: false });
  };

  const closeBlockMenu = () => {
    blockMenuOpenRef.current = false;
    setBlockMenuAnchor(undefined);
  };

  const copyText = async (value: string) => {
    if (!value) return;
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return;
    }
    const input = document.createElement("textarea");
    input.value = value;
    input.style.position = "fixed";
    input.style.opacity = "0";
    document.body.append(input);
    input.select();
    document.execCommand("copy");
    input.remove();
  };

  const runBlockAction = (
    action:
      ArticleBlockConversion | "after" | "before" | "up" | "down" | "duplicate",
  ) => {
    const position = blockMenuAnchor?.pos;
    if (position === undefined) return;
    if (action === "before" || action === "after") {
      insertArticleBlock(editor, position, action);
    } else if (action === "up" || action === "down") {
      moveArticleBlock(editor, position, action);
    } else if (action === "duplicate") {
      duplicateArticleBlock(editor, position);
    } else {
      convertArticleBlock(editor, position, action);
    }
    closeBlockMenu();
  };

  const deleteBlockFromMenu = () => {
    const position = blockMenuAnchor?.pos;
    if (position === undefined) return;
    closeBlockMenu();
    deleteArticleBlock(editor, position);
  };

  const copyBlockId = () => {
    const position = blockMenuAnchor?.pos;
    const id =
      position === undefined ? "" : String(blockAt(position)?.attrs.id ?? "");
    void copyText(id)
      .then(() => {
        if (id) setDropError("块 ID 已复制");
      })
      .catch(() => setDropError("复制块 ID 失败"));
  };

  const cutBlock = () => {
    const position = blockMenuAnchor?.pos;
    if (position === undefined) return;
    const node = editor.state.doc.nodeAt(position);
    if (!node) return;
    const value =
      node.textContent.trim() ||
      String(node.attrs.title ?? node.attrs.alt ?? node.type.name);
    void copyText(value)
      .then(() => {
        closeBlockMenu();
        deleteArticleBlock(editor, position);
      })
      .catch(() => setDropError("无法访问系统剪贴板，未删除该块"));
  };

  const reviewCurrentBlock = async (blockId: string) => {
    let current: ProseMirrorNode | undefined;
    editor.state.doc.forEach((node) => {
      if (String(node.attrs.id ?? "") === blockId) current = node;
    });
    if (!current) throw new Error("该块已经变化，请重新选择后审阅");
    await onReviewBlock(
      blockId,
      await articleBlockContentFingerprint(current.toJSON()),
    );
  };

  const reviewBlockFromMenu = () => {
    const position = blockMenuAnchor?.pos;
    const id =
      position === undefined ? "" : String(blockAt(position)?.attrs.id ?? "");
    if (!id) return;
    closeBlockMenu();
    void reviewCurrentBlock(id).catch((error: unknown) => {
      setDropError(error instanceof Error ? error.message : "审阅失败");
    });
  };

  const selectBlockFromHandle = () => {
    const blockPos = dragHandlePos.current;
    if (blockPos === null || !editor.state.doc.nodeAt(blockPos)) return;
    selectArticleBlock(editor, blockPos, { scrollIntoView: false });
  };

  const startPointerBlockDrag = (
    event: ReactPointerEvent<HTMLButtonElement>,
  ) => {
    if (event.button !== 0 || !event.isPrimary) return;
    const position = dragHandlePos.current;
    if (position === null || !editor.state.doc.nodeAt(position)) return;

    event.preventDefault();
    event.stopPropagation();
    blockPointerDragCleanupRef.current();

    const selection = editor.state.selection;
    const selectedRange =
      isNodeRangeSelection(selection) &&
      position >= selection.from &&
      position < selection.to
        ? { from: selection.from, to: selection.to }
        : null;
    if (!selectedRange) {
      selectArticleBlock(editor, position, { scrollIntoView: false });
    }

    const dragHandle = event.currentTarget.closest<HTMLElement>(".drag-handle");
    if (dragHandle) dragHandle.draggable = false;
    event.currentTarget.setPointerCapture(event.pointerId);

    blockPointerDragActiveRef.current = true;
    blockDraggingPosRef.current = position;
    blockDraggingRangeRef.current = selectedRange;
    dropIndicatorRef.current = undefined;
    lastDragCoordsRef.current = {
      clientX: event.clientX,
      clientY: event.clientY,
    };
    lastDragOptionsRef.current = { autoScroll: true, dropEffect: "move" };
    setBlockDraggingPos(position);
    setDropIndicator(undefined);

    const clearPointerDrag = () => {
      window.removeEventListener("pointermove", pointerMove, true);
      window.removeEventListener("pointerup", pointerUp, true);
      window.removeEventListener("pointercancel", pointerCancel, true);
      if (dragHandle) dragHandle.draggable = true;
      blockPointerDragActiveRef.current = false;
      blockPointerDragCleanupRef.current = () => undefined;
    };
    const finishPointerDrag = (moveBlock: boolean) => {
      const insertionPosition = dropIndicatorRef.current?.position;
      const draggingPosition = blockDraggingPosRef.current;
      const range = blockDraggingRangeRef.current;

      clearPointerDrag();
      blockDraggingPosRef.current = null;
      blockDraggingRangeRef.current = null;
      dropIndicatorRef.current = undefined;
      setBlockDraggingPos(undefined);
      setDropIndicator(undefined);
      stopAutoScroll();

      if (
        !moveBlock ||
        insertionPosition === undefined ||
        draggingPosition === null
      ) {
        return;
      }
      if (range) {
        moveArticleBlockRange(editor, range.from, range.to, insertionPosition);
        return;
      }
      const draggedNode = editor.state.doc.nodeAt(draggingPosition);
      if (!draggedNode) return;
      moveArticleBlockRange(
        editor,
        draggingPosition,
        draggingPosition + draggedNode.nodeSize,
        insertionPosition,
      );
    };
    const pointerMove = (pointerEvent: globalThis.PointerEvent) => {
      if (pointerEvent.pointerId !== event.pointerId) return;
      pointerEvent.preventDefault();
      const surface = editorSurfaceRef.current;
      if (!surface) return;
      const rect = surface.getBoundingClientRect();
      if (
        pointerEvent.clientX < rect.left ||
        pointerEvent.clientX > rect.right ||
        pointerEvent.clientY < rect.top ||
        pointerEvent.clientY > rect.bottom
      ) {
        clearDropIndicator();
        stopAutoScroll();
        return;
      }
      const syntheticDragEvent = {
        clientX: pointerEvent.clientX,
        clientY: pointerEvent.clientY,
        dataTransfer: { dropEffect: "move", types: [] },
        preventDefault: () => undefined,
      } as unknown as globalThis.DragEvent;
      updateDropIndicatorRef.current?.(syntheticDragEvent, {
        autoScroll: true,
        dropEffect: "move",
      });
    };
    const pointerUp = (pointerEvent: globalThis.PointerEvent) => {
      if (pointerEvent.pointerId !== event.pointerId) return;
      pointerEvent.preventDefault();
      finishPointerDrag(true);
    };
    const pointerCancel = (pointerEvent: globalThis.PointerEvent) => {
      if (pointerEvent.pointerId !== event.pointerId) return;
      finishPointerDrag(false);
    };

    blockPointerDragCleanupRef.current = () => finishPointerDrag(false);
    window.addEventListener("pointermove", pointerMove, {
      capture: true,
      passive: false,
    });
    window.addEventListener("pointerup", pointerUp, {
      capture: true,
      passive: false,
    });
    window.addEventListener("pointercancel", pointerCancel, { capture: true });
  };

  const locateBlock = (blockId: string) => {
    let targetPosition: number | undefined;
    editor.state.doc.descendants((node, pos) => {
      if (String(node.attrs.id ?? "") === blockId) {
        targetPosition = pos;
        return false;
      }
    });
    if (targetPosition === undefined) return;
    const dom = editor.view.nodeDOM(targetPosition);
    if (dom instanceof HTMLElement)
      dom.scrollIntoView({ behavior: "smooth", block: "center" });
    editor.view.dispatch(
      editor.state.tr.setSelection(
        NodeSelection.create(editor.state.doc, targetPosition),
      ),
    );
  };

  const updateHoverMenu = (event: MouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement;
    if (target.closest("[data-article-node-menu]")) return;
    const node = target.closest(
      "table, figure[data-article-image], [data-article-artifact-image], figure[data-article-image-group]",
    );
    const surface = editorSurfaceRef.current;
    if (!node || !surface) {
      if (!menuOpen && hoverMenu) setHoverMenu(undefined);
      return;
    }
    const rect = node.getBoundingClientRect();
    let pos: number;
    try {
      pos = editor.view.posAtDOM(node, 0);
    } catch {
      return;
    }
    const nextHoverMenu = {
      kind: node.matches("figure[data-article-image-group]")
        ? "imageGroup"
        : node.matches(
              "figure[data-article-image], [data-article-artifact-image]",
            )
          ? "image"
          : "table",
      left: Math.max(36, Math.min(rect.right - 28, window.innerWidth - 8)),
      placement: rect.top > window.innerHeight / 2 ? "above" : "below",
      pos,
      top: Math.max(8, Math.min(rect.top + 4, window.innerHeight - 36)),
    } as const;
    setHoverMenu((current) =>
      current?.kind === nextHoverMenu.kind &&
      current.pos === nextHoverMenu.pos &&
      current.placement === nextHoverMenu.placement &&
      Math.abs(current.left - nextHoverMenu.left) < 0.5 &&
      Math.abs(current.top - nextHoverMenu.top) < 0.5
        ? current
        : nextHoverMenu,
    );
  };

  const updateTableEdgeHandle = (event: MouseEvent<HTMLDivElement>) => {
    if (!canEdit || tableEdgeMenuOpen || tableDragSessionRef.current) return;
    const tables = Array.from(editor.view.dom.querySelectorAll("table"));
    let next: ArticleTableEdgeHandle | undefined;
    for (const table of tables) {
      const tableRect = table.getBoundingClientRect();
      const nearLeft = Math.abs(event.clientX - tableRect.left) <= 28;
      const nearTop = Math.abs(event.clientY - tableRect.top) <= 24;
      if (!nearLeft && !nearTop) continue;
      const rows = Array.from(table.rows);
      const firstRow = rows[0];
      const rowIndex = rows.findIndex((row) => {
        const rect = row.getBoundingClientRect();
        return event.clientY >= rect.top && event.clientY <= rect.bottom;
      });
      const cells = firstRow ? Array.from(firstRow.cells) : [];
      const columnIndex = cells.findIndex((cell) => {
        const rect = cell.getBoundingClientRect();
        return event.clientX >= rect.left && event.clientX <= rect.right;
      });
      const axis = nearLeft && rowIndex >= 0 ? "row" : "column";
      const index = axis === "row" ? rowIndex : columnIndex;
      const cell =
        axis === "row" ? rows[index]?.cells[0] : firstRow?.cells[index];
      if (index < 0 || !cell) continue;
      let cellPos: number;
      try {
        cellPos = editor.view.posAtDOM(cell, 0);
      } catch {
        continue;
      }
      const resolved = editor.state.doc.resolve(
        Math.min(cellPos + 1, editor.state.doc.content.size),
      );
      let tablePos: number | undefined;
      for (let depth = resolved.depth; depth > 0; depth -= 1) {
        if (resolved.node(depth).type.name === "table") {
          tablePos = resolved.before(depth);
          break;
        }
      }
      if (tablePos === undefined) continue;
      const targetRect =
        axis === "row"
          ? rows[index]!.getBoundingClientRect()
          : cells[index]!.getBoundingClientRect();
      next = {
        axis,
        cellPos,
        index,
        left:
          axis === "row"
            ? Math.max(4, tableRect.left - 28)
            : targetRect.left + targetRect.width / 2 - 12,
        tablePos,
        top:
          axis === "row"
            ? targetRect.top + targetRect.height / 2 - 12
            : Math.max(4, tableRect.top - 28),
      };
      break;
    }
    setTableEdgeHandle((current) =>
      current?.axis === next?.axis &&
      current?.cellPos === next?.cellPos &&
      current?.index === next?.index &&
      current?.tablePos === next?.tablePos &&
      Math.abs((current?.left ?? 0) - (next?.left ?? 0)) < 0.5 &&
      Math.abs((current?.top ?? 0) - (next?.top ?? 0)) < 0.5
        ? current
        : next,
    );
  };

  const findNode = (kind: ArticleNodeMenuKind) => {
    if (!hoverMenu) return;
    const names =
      kind === "table"
        ? new Set(["table"])
        : kind === "imageGroup"
          ? new Set(["articleImageGroup"])
          : new Set(["articleImage", "artifactReference"]);
    const direct = editor.state.doc.nodeAt(hoverMenu.pos);
    if (direct && names.has(direct.type.name)) {
      return { node: direct, pos: hoverMenu.pos };
    }
    const safePosition = Math.min(
      hoverMenu.pos + 1,
      editor.state.doc.content.size,
    );
    const resolved = editor.state.doc.resolve(safePosition);
    for (let depth = resolved.depth; depth > 0; depth -= 1) {
      const node = resolved.node(depth);
      const nodePos = resolved.before(depth);
      if (names.has(node.type.name)) return { node, pos: nodePos };
    }
  };

  const currentAlignment = (): ArticleImageAlignment => {
    const located = findNode("image");
    return located?.node.attrs.align === "left" ||
      located?.node.attrs.align === "right"
      ? located.node.attrs.align
      : "center";
  };

  const currentImageWidth = () => {
    const located = findNode("image");
    return normalizeImageWidth(located?.node.attrs.width);
  };

  const currentImageGroupColumns = () => {
    const located = findNode("imageGroup");
    return normalizeArticleImageGroupColumns(located?.node.attrs.columns);
  };

  const setImageGroupColumns = (columns: number) => {
    const located = findNode("imageGroup");
    if (!located) return;
    editor.view.dispatch(
      editor.state.tr.setNodeMarkup(located.pos, undefined, {
        ...located.node.attrs,
        columns: normalizeArticleImageGroupColumns(columns),
      }),
    );
  };

  const saveCaption = (kind: ArticleNodeMenuKind) => {
    const located = findNode(kind);
    if (!located) return;
    const caption = captionDraft.trim();
    editor.view.dispatch(
      editor.state.tr.setNodeMarkup(located.pos, undefined, {
        ...located.node.attrs,
        caption,
      }),
    );
    setMenuOpen(false);
  };

  const saveAlt = () => {
    const located = findNode("image");
    if (!located) return;
    editor.view.dispatch(
      editor.state.tr.setNodeMarkup(located.pos, undefined, {
        ...located.node.attrs,
        alt: altDraft.trim(),
      }),
    );
  };

  const alignImage = (alignment: ArticleImageAlignment) => {
    const located = findNode("image");
    if (!located) return;
    editor.view.dispatch(
      editor.state.tr.setNodeMarkup(located.pos, undefined, {
        ...located.node.attrs,
        align: alignment,
      }),
    );
  };

  const resizeImage = (width: number) => {
    const located = findNode("image");
    if (!located) return;
    editor.view.dispatch(
      editor.state.tr.setNodeMarkup(located.pos, undefined, {
        ...located.node.attrs,
        width: normalizeImageWidth(width),
      }),
    );
  };

  const openImageSource = async (download: boolean) => {
    const located = findNode("image");
    if (!located) return;
    let url = String(located.node.attrs.src ?? "").trim();
    if (located.node.type.name === "artifactReference") {
      const artifactId = String(
        located.node.attrs.artifactId ?? located.node.attrs.objectId ?? "",
      );
      const versionId = String(located.node.attrs.versionId ?? "");
      if (!download) {
        window.open(
          `/projects/${encodeURIComponent(projectId)}/artifacts?artifact=${encodeURIComponent(artifactId)}`,
          "_blank",
          "noopener,noreferrer",
        );
        return;
      }
      const grant = await artifactApi.download(
        projectId,
        artifactId,
        versionId,
      );
      url = grant.transfer.url;
    }
    if (!url) {
      setDropError("图片源文件不可用");
      return;
    }
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.rel = "noopener noreferrer";
    anchor.target = "_blank";
    if (download) anchor.download = String(located.node.attrs.alt ?? "image");
    anchor.click();
  };

  const replaceImage = () => {
    const located = findNode("image");
    const src = replaceDraft.trim();
    if (!located || !src) return;
    if (isTransientImageURL(src)) {
      setDropError("不能把临时签名地址写入论文；请上传图片或拖入 Artifact。");
      return;
    }
    if (located.node.type.name === "articleImage") {
      editor.view.dispatch(
        editor.state.tr.setNodeMarkup(located.pos, undefined, {
          ...located.node.attrs,
          src,
        }),
      );
    } else {
      const image = editor.schema.nodes.articleImage.create({
        align: located.node.attrs.align ?? "center",
        alt: located.node.attrs.alt ?? "",
        caption: located.node.attrs.caption ?? "",
        src,
        width: located.node.attrs.width ?? 100,
      });
      editor.view.dispatch(
        editor.state.tr.replaceWith(
          located.pos,
          located.pos + located.node.nodeSize,
          image,
        ),
      );
    }
    setReplaceDraft("");
  };

  const replaceImageFromFile = () => {
    const located = findNode("image");
    if (!located) return;
    replacementImageTargetRef.current = {
      blockId: String(located.node.attrs.id ?? ""),
      fallbackPosition: located.pos,
    };
    replacementImageInputRef.current?.click();
  };

  const deleteImage = () => {
    const located = findNode("image");
    if (!located) return;
    deleteArticleImageNode(editor, located.pos);
    setHoverMenu(undefined);
    setMenuOpen(false);
  };

  const currentImageGroupContext = () => {
    const located = findNode("image");
    return located ? articleImageGroupContext(editor, located.pos) : undefined;
  };

  const runImageGroupAction = (action: ArticleImageGroupAction) => {
    const changed =
      action === "ungroup"
        ? (() => {
            const located = findNode("imageGroup");
            return located ? ungroupArticleImages(editor, located.pos) : false;
          })()
        : (() => {
            const located = findNode("image");
            if (!located) return false;
            if (action === "removeFromGroup") {
              return removeArticleImageFromGroup(editor, located.pos);
            }
            if (action === "moveEarlier") {
              return moveArticleImageInGroupDirection(
                editor,
                located.pos,
                "earlier",
              );
            }
            if (action === "moveLater") {
              return moveArticleImageInGroupDirection(
                editor,
                located.pos,
                "later",
              );
            }
            return mergeArticleImageWithNeighbor(
              editor,
              located.pos,
              action === "mergeBefore" ? "before" : "after",
            );
          })();
    if (!changed) return;
    setHoverMenu(undefined);
    setMenuOpen(false);
  };

  const findTableAtSelection = () => {
    for (
      let depth = editor.state.selection.$from.depth;
      depth > 0;
      depth -= 1
    ) {
      if (editor.state.selection.$from.node(depth).type.name === "table") {
        return {
          node: editor.state.selection.$from.node(depth),
          pos: editor.state.selection.$from.before(depth),
        };
      }
    }
  };

  const openMenuForNode = (kind: ArticleNodeMenuKind, pos: number) => {
    const dom = editor.view.nodeDOM(pos);
    const surface = editorSurfaceRef.current;
    if (!(dom instanceof HTMLElement) || !surface) return;
    const target = kind === "table" ? (dom.querySelector("table") ?? dom) : dom;
    const rect = target.getBoundingClientRect();
    const node = editor.state.doc.nodeAt(pos);
    setHoverMenu({
      kind,
      left: Math.max(36, Math.min(rect.right - 28, window.innerWidth - 8)),
      placement: rect.top > window.innerHeight / 2 ? "above" : "below",
      pos,
      top: Math.max(8, Math.min(rect.top + 4, window.innerHeight - 36)),
    });
    setCaptionDraft(String(node?.attrs.caption ?? ""));
    setAltDraft(String(node?.attrs.alt ?? ""));
    setReplaceDraft("");
    setMenuOpen(true);
  };

  const openCurrentMenu = () => {
    if (!hoverMenu) return;
    const located = findNode(hoverMenu.kind);
    if (!located) return;
    setCaptionDraft(String(located.node.attrs.caption ?? ""));
    setAltDraft(String(located.node.attrs.alt ?? ""));
    setReplaceDraft("");
    setMenuOpen((value) => !value);
  };

  const runTableCommand = (command: TableAction) => {
    if (!hoverMenu || hoverMenu.kind !== "table") return;
    const located = findNode("table");
    if (!located) return;

    if (command === "equalizeColumns") {
      const cellWidths = equalizedArticleTableCellWidths(located.node);
      if (cellWidths.size === 0) return;

      let transaction = editor.state.tr;
      for (const [relativePosition, colwidth] of cellWidths) {
        const cell = located.node.nodeAt(relativePosition);
        if (!cell) continue;
        transaction = transaction.setNodeMarkup(
          located.pos + 1 + relativePosition,
          cell.type,
          { ...cell.attrs, colwidth },
        );
      }
      editor.view.dispatch(transaction);
      setMenuOpen(false);
      return;
    }

    let cellTextPosition: number | undefined;
    located.node.descendants((node, relativePosition) => {
      if (cellTextPosition === undefined && node.isTextblock) {
        // descendants() positions are relative to the table content. Move
        // through the table and paragraph opening tokens so table commands
        // receive a real selection inside the first cell.
        cellTextPosition = located.pos + relativePosition + 2;
        return false;
      }
    });
    if (cellTextPosition === undefined) return;
    editor.view.dispatch(
      editor.state.tr.setSelection(
        TextSelection.near(editor.state.doc.resolve(cellTextPosition), 1),
      ),
    );
    const chain = editor.chain().focus();
    chain[command]().run();
  };

  const focusTableEdgeCell = (handle: ArticleTableEdgeHandle) => {
    const position = Math.min(
      handle.cellPos + 1,
      editor.state.doc.content.size,
    );
    editor.view.dispatch(
      editor.state.tr.setSelection(
        TextSelection.near(editor.state.doc.resolve(position), 1),
      ),
    );
  };

  const runTableEdgeAction = (action: ArticleTableEdgeAction) => {
    if (!tableEdgeHandle) return;
    focusTableEdgeCell(tableEdgeHandle);
    const command =
      tableEdgeHandle.axis === "row"
        ? action === "addBefore"
          ? "addRowBefore"
          : action === "addAfter"
            ? "addRowAfter"
            : "deleteRow"
        : action === "addBefore"
          ? "addColumnBefore"
          : action === "addAfter"
            ? "addColumnAfter"
            : "deleteColumn";
    editor.chain().focus()[command]().run();
    setTableEdgeMenuOpen(false);
    setTableEdgeHandle(undefined);
  };

  const reorderTableEdge = (
    handle: ArticleTableEdgeHandle,
    targetIndex: number,
  ) => {
    const table = editor.state.doc.nodeAt(handle.tablePos);
    if (!table || table.type.name !== "table") return;
    const rows: ProseMirrorNode[] = [];
    table.forEach((row) => rows.push(row));
    let replacement: ProseMirrorNode;
    if (handle.axis === "row") {
      replacement = table.type.create(
        table.attrs,
        moveArrayItem(rows, handle.index, targetIndex),
        table.marks,
      );
    } else {
      if (
        rows.some(
          (row) =>
            handle.index >= row.childCount || targetIndex >= row.childCount,
        )
      )
        return;
      const reorderedRows = rows.map((row) => {
        const cells: ProseMirrorNode[] = [];
        row.forEach((cell) => cells.push(cell));
        return row.type.create(
          row.attrs,
          moveArrayItem(cells, handle.index, targetIndex),
          row.marks,
        );
      });
      replacement = table.type.create(table.attrs, reorderedRows, table.marks);
    }
    editor.view.dispatch(
      editor.state.tr
        .replaceWith(
          handle.tablePos,
          handle.tablePos + table.nodeSize,
          replacement,
        )
        .scrollIntoView(),
    );
  };

  const startTableEdgeDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!tableEdgeHandle || event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    tableDragCleanupRef.current();
    const session: TableDragSession = {
      active: false,
      handle: tableEdgeHandle,
      startX: event.clientX,
      startY: event.clientY,
      targetIndex: tableEdgeHandle.index,
    };
    tableDragSessionRef.current = session;
    const pointerMove = (pointerEvent: PointerEvent) => {
      const current = tableDragSessionRef.current;
      if (!current) return;
      if (
        !current.active &&
        Math.hypot(
          pointerEvent.clientX - current.startX,
          pointerEvent.clientY - current.startY,
        ) < 4
      )
        return;
      current.active = true;
      pointerEvent.preventDefault();
      document.body.classList.add("article-table-edge-dragging");
      const tableDom = editor.view.nodeDOM(current.handle.tablePos);
      const table =
        tableDom instanceof HTMLElement
          ? tableDom.matches("table")
            ? tableDom
            : tableDom.querySelector("table")
          : null;
      if (!(table instanceof HTMLTableElement)) return;
      const tableRect = table.getBoundingClientRect();
      const elements =
        current.handle.axis === "row"
          ? Array.from(table.rows)
          : Array.from(table.rows[0]?.cells ?? []);
      if (!elements.length) return;
      const coordinate =
        current.handle.axis === "row"
          ? pointerEvent.clientY
          : pointerEvent.clientX;
      let targetIndex = 0;
      let smallestDistance = Number.POSITIVE_INFINITY;
      elements.forEach((element, index) => {
        const rect = element.getBoundingClientRect();
        const center =
          current.handle.axis === "row"
            ? rect.top + rect.height / 2
            : rect.left + rect.width / 2;
        const distance = Math.abs(coordinate - center);
        if (distance < smallestDistance) {
          smallestDistance = distance;
          targetIndex = index;
        }
      });
      current.targetIndex = targetIndex;
      const targetRect = elements[targetIndex]!.getBoundingClientRect();
      const after = targetIndex > current.handle.index;
      setTableDragIndicator({
        axis: current.handle.axis,
        left:
          current.handle.axis === "row"
            ? tableRect.left
            : after
              ? targetRect.right
              : targetRect.left,
        length:
          current.handle.axis === "row" ? tableRect.width : tableRect.height,
        top:
          current.handle.axis === "row"
            ? after
              ? targetRect.bottom
              : targetRect.top
            : tableRect.top,
      });
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", pointerMove);
      window.removeEventListener("pointerup", pointerUp);
      window.removeEventListener("pointercancel", pointerUp);
      document.body.classList.remove("article-table-edge-dragging");
      tableDragSessionRef.current = undefined;
      setTableDragIndicator(undefined);
      tableDragCleanupRef.current = () => undefined;
    };
    const pointerUp = () => {
      const current = tableDragSessionRef.current;
      if (current?.active) {
        reorderTableEdge(current.handle, current.targetIndex);
        suppressTableHandleClickRef.current = true;
        setTableEdgeHandle(undefined);
        setTableEdgeMenuOpen(false);
      }
      cleanup();
    };
    tableDragCleanupRef.current = cleanup;
    window.addEventListener("pointermove", pointerMove, { passive: false });
    window.addEventListener("pointerup", pointerUp);
    window.addEventListener("pointercancel", pointerUp);
  };

  const toggleTableEdgeMenu = () => {
    if (suppressTableHandleClickRef.current) {
      suppressTableHandleClickRef.current = false;
      return;
    }
    setTableEdgeMenuOpen((current) => !current);
  };

  const updateInternalDragOver = (event: DragEvent<HTMLDivElement>) => {
    const draggingPosition = blockDraggingPosRef.current;
    if (draggingPosition === null) return false;
    return updateDropIndicator(event, {
      autoScroll: true,
      dropEffect: "move",
    });
  };

  const updateExternalArtifactDragOver = (event: DragEvent<HTMLDivElement>) => {
    if (
      !canEdit ||
      (!event.dataTransfer.types.includes(articleArtifactMime) &&
        !event.dataTransfer.types.includes("Files"))
    ) {
      return false;
    }
    if (inlineDropIndicatorRef.current !== undefined) {
      inlineDropIndicatorRef.current = undefined;
      setInlineDropIndicator(undefined);
    }
    return updateDropIndicator(event, {
      autoScroll: true,
      dropEffect: "copy",
    });
  };

  const updateZoteroDragOver = (event: DragEvent<HTMLDivElement>) => {
    if (!canEdit || !event.dataTransfer.types.includes(articleZoteroMime)) {
      return false;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";

    if (dropIndicatorRef.current !== undefined) {
      dropIndicatorRef.current = undefined;
      setDropIndicator(undefined);
    }

    const surface = editorSurfaceRef.current;
    if (!surface) return true;

    const target = editor.view.posAtCoords({
      left: event.clientX,
      top: event.clientY,
    });
    if (!target) {
      inlineDropIndicatorRef.current = undefined;
      setInlineDropIndicator(undefined);
      return true;
    }

    const coords = editor.view.coordsAtPos(target.pos);
    const surfaceRect = surface.getBoundingClientRect();
    const left = coords.left - surfaceRect.left + surface.scrollLeft;
    const top = coords.top - surfaceRect.top + surface.scrollTop;
    const height = Math.max(16, coords.bottom - coords.top);

    const indicator = {
      height,
      left,
      pos: target.pos,
      top,
    };
    inlineDropIndicatorRef.current = indicator;
    setInlineDropIndicator(indicator);

    return true;
  };

  const mathEditorNode = mathEditorTarget
    ? editor.state.doc.nodeAt(mathEditorTarget.pos)
    : undefined;
  const mathEditorDom = mathEditorTarget
    ? editor.view.nodeDOM(mathEditorTarget.pos)
    : undefined;
  const mathEditorAnchor =
    mathEditorTarget &&
    mathEditorNode?.type.name ===
      (mathEditorTarget.kind === "block" ? "blockMath" : "inlineMath") &&
    mathEditorDom instanceof HTMLElement
      ? (() => {
          const rect = mathEditorDom.getBoundingClientRect();
          const placement = rect.top > 220 ? "above" : "below";
          return {
            left: Math.max(8, Math.min(rect.left, window.innerWidth - 392)),
            placement,
            top: placement === "above" ? rect.top - 8 : rect.bottom + 8,
          } as const;
        })()
      : undefined;
  const themeClass =
    renderTheme === "latex" ? "article-theme-latex" : "article-theme-md";
  const blockMenuNode = blockMenuAnchor
    ? blockAt(blockMenuAnchor.pos)
    : undefined;
  const blockMenuId = String(blockMenuNode?.attrs.id ?? "");
  const blockMenuMetadata = blocks.find(
    (block) => block.block_id === blockMenuId,
  );
  const blockMenuIndex =
    blockMenuAnchor === undefined
      ? -1
      : editor.state.doc.resolve(blockMenuAnchor.pos).index(0);

  return (
    <div
      className="flex h-full min-h-0 flex-1 flex-col overflow-hidden rounded-lg border bg-background shadow-sm"
      data-article-editor-shell
      onDragOver={(event) => {
        if (
          event.dataTransfer.types.includes(
            "application/vnd.mmdash.image-group-item",
          )
        ) {
          clearDropIndicator();
          return;
        }
        const targetEl =
          typeof document !== "undefined"
            ? document.elementFromPoint(event.clientX, event.clientY)
            : null;
        if (
          targetEl?.closest?.(
            "[data-article-image-group], figure[data-article-image], [data-article-artifact-image]",
          )
        ) {
          clearDropIndicator();
          return;
        }
        if (updateInternalDragOver(event)) return;
        if (updateZoteroDragOver(event)) return;
        if (updateExternalArtifactDragOver(event)) return;
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node)) {
          clearDropIndicator();
          stopAutoScroll();
        }
      }}
      onDrop={(event) => {
        stopAutoScroll();
        if (
          event.dataTransfer.types.includes(
            "application/vnd.mmdash.image-group-item",
          )
        ) {
          clearDropIndicator();
          return;
        }
        const targetEl =
          typeof document !== "undefined"
            ? document.elementFromPoint(event.clientX, event.clientY)
            : null;
        if (
          targetEl?.closest?.(
            "[data-article-image-group], figure[data-article-image], [data-article-artifact-image]",
          )
        ) {
          clearDropIndicator();
          return;
        }
        const insertionPosition = dropIndicatorRef.current?.position;
        const inlinePosition = inlineDropIndicatorRef.current?.pos;
        clearDropIndicator();
        const localImage = droppedLocalImage(event.dataTransfer);
        if (localImage) {
          event.preventDefault();
          void uploadImage(localImage, undefined, insertionPosition);
          return;
        }
        void dropArtifact(event, inlinePosition ?? insertionPosition);
      }}
    >
      <div className="sticky top-0 z-30 flex shrink-0 flex-wrap items-center gap-1 border-b bg-background/95 p-2 backdrop-blur">
        <EditorButton
          label="加粗"
          onClick={() => editor.chain().focus().toggleBold().run()}
        >
          <Bold />
        </EditorButton>
        <EditorButton
          label="斜体"
          onClick={() => editor.chain().focus().toggleItalic().run()}
        >
          <Italic />
        </EditorButton>
        <EditorButton
          label="二级标题"
          onClick={() =>
            editor.chain().focus().toggleHeading({ level: 2 }).run()
          }
        >
          <Heading2 />
        </EditorButton>
        <EditorButton
          label="代码块"
          onClick={() => editor.chain().focus().toggleCodeBlock().run()}
        >
          <Code2 />
        </EditorButton>
        <EditorButton label="公式块" onClick={insertMath}>
          <Sigma />
        </EditorButton>
        <EditorButton
          label="上传图片并插入"
          onClick={() => imageInputRef.current?.click()}
        >
          <ImagePlus />
        </EditorButton>
        <input
          accept="image/*"
          aria-label="选择要上传的图片"
          className="sr-only"
          onChange={selectImage}
          ref={imageInputRef}
          type="file"
        />
        <input
          accept="image/*"
          aria-label="选择用于替换的图片"
          className="sr-only"
          onChange={selectReplacementImage}
          ref={replacementImageInputRef}
          type="file"
        />
        <EditorButton label="添加表注" onClick={insertTableCaption}>
          <Captions />
        </EditorButton>
        <EditorButton
          label="插入表格"
          onClick={() =>
            editor
              .chain()
              .focus()
              .insertTable({ rows: 3, cols: 3, withHeaderRow: true })
              .run()
          }
        >
          <Table2 />
        </EditorButton>
        <EditorButton
          label="撤销"
          onClick={() => editor.chain().focus().undo().run()}
        >
          <Undo2 />
        </EditorButton>
        <div className="ml-auto flex items-center gap-1.5">
          {immersive && draftRevision !== undefined ? (
            <Badge className="text-xs font-normal">草稿 r{draftRevision}</Badge>
          ) : null}
          {immersive && onOpenCommit ? (
            <Button
              className="h-8 text-xs"
              disabled={!canCommit}
              onClick={onOpenCommit}
              size="sm"
              variant="default"
            >
              Commit…
            </Button>
          ) : null}
          <Button
            className="h-8 text-xs gap-1"
            onClick={onFlush}
            size="sm"
            variant="outline"
          >
            <Save className="size-3.5" />
            保存同步
          </Button>
          {onToggleImmersive ? (
            <Button
              aria-label={immersive ? "退出沉浸模式 (Esc)" : "进入沉浸编辑模式"}
              className="h-8 gap-1 px-2 text-xs"
              onClick={onToggleImmersive}
              size="sm"
              title={immersive ? "退出沉浸模式 (Esc)" : "沉浸编辑模式"}
              variant={immersive ? "secondary" : "ghost"}
            >
              {immersive ? (
                <>
                  <Minimize2 className="size-3.5" />
                  <span className="hidden sm:inline">退出沉浸</span>
                </>
              ) : (
                <>
                  <Maximize2 className="size-3.5" />
                  <span className="hidden sm:inline">沉浸模式</span>
                </>
              )}
            </Button>
          ) : null}
        </div>
      </div>
      {!canEdit ? (
        <p className="border-b bg-muted/20 px-4 py-2 text-xs text-muted-foreground">
          你当前拥有只读权限；协同光标与远端更新仍会保持同步。
        </p>
      ) : null}
      {dropError ? (
        <div
          className="flex items-center gap-2 border-b bg-destructive/5 px-4 py-2 text-xs text-destructive"
          role="status"
        >
          <span className="min-w-0 flex-1">{dropError}</span>
          <button
            aria-label="关闭上传提示"
            className="flex size-6 shrink-0 items-center justify-center rounded hover:bg-destructive/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => setDropError(undefined)}
            type="button"
          >
            <X className="size-3.5" />
          </button>
        </div>
      ) : null}
      <div className="relative min-h-0 flex-1">
        <div
          className="relative h-full overflow-y-auto overscroll-contain pl-24 pr-14"
          data-article-block-dragging={
            blockDraggingPos === undefined ? undefined : "true"
          }
          onMouseMove={(event) => {
            updateHoverMenu(event);
            updateTableEdgeHandle(event);
          }}
          onPaste={pasteImage}
          onScroll={() => {
            setTableEdgeHandle(undefined);
            setTableEdgeMenuOpen(false);
            setMathEditorTarget(undefined);
            if (!menuOpen) setHoverMenu(undefined);
          }}
          onMouseLeave={(event) => {
            const relatedTarget = event.relatedTarget;
            if (
              relatedTarget instanceof Element &&
              relatedTarget.closest(
                "[data-article-node-menu], [data-article-table-controls]",
              )
            )
              return;
            if (!menuOpen) setHoverMenu(undefined);
            if (!tableEdgeMenuOpen && !tableDragSessionRef.current)
              setTableEdgeHandle(undefined);
          }}
          ref={editorSurfaceRef}
          style={{ scrollbarGutter: "stable" }}
        >
          <div
            aria-hidden="true"
            className="pointer-events-none absolute left-0 top-0 z-30 border border-primary/70 bg-primary/15"
            data-article-marquee-selection
            hidden
            ref={marqueeRef}
          />
          {dropIndicator ? (
            <div
              aria-live="polite"
              className="pointer-events-none absolute left-24 right-8 z-20 flex items-center gap-2 text-[11px] text-primary"
              data-article-drop-indicator
              style={{ top: dropIndicator.top }}
            >
              <span className="h-0.5 flex-1 rounded-full bg-primary shadow-sm" />
              <span className="rounded bg-background px-1.5 py-0.5 shadow-sm">
                {dropIndicator.label}
              </span>
            </div>
          ) : null}
          {inlineDropIndicator ? (
            <div
              aria-hidden="true"
              className="pointer-events-none absolute z-30 w-0.5 rounded-full bg-primary shadow-xs animate-pulse"
              data-article-inline-drop-indicator
              style={{
                height: `${inlineDropIndicator.height}px`,
                left: `${inlineDropIndicator.left}px`,
                top: `${inlineDropIndicator.top}px`,
              }}
            />
          ) : null}
          {canEdit ? (
            <DragHandle
              editor={editor}
              onElementDragEnd={handleElementDragEnd}
              onElementDragStart={handleElementDragStart}
              onNodeChange={handleDragNodeChange}
            >
              <div className="flex items-center gap-0.5 rounded-md border bg-background/95 p-0.5 text-muted-foreground shadow-sm">
                <button
                  aria-label="在当前块前插入新块"
                  className="flex size-6 items-center justify-center rounded hover:bg-muted hover:text-foreground"
                  onClick={(event) => {
                    event.stopPropagation();
                    insertParagraphBeforeCurrent();
                  }}
                  type="button"
                >
                  <Plus className="size-3.5" />
                </button>
                <button
                  aria-label="拖动当前块排序"
                  className="flex size-6 cursor-grab items-center justify-center rounded hover:bg-muted hover:text-foreground active:cursor-grabbing"
                  onPointerDown={startPointerBlockDrag}
                  onClick={(event) => {
                    event.stopPropagation();
                    selectBlockFromHandle();
                  }}
                  type="button"
                >
                  <GripVertical className="size-3.5" />
                </button>
                <button
                  aria-label="打开块操作菜单"
                  className="flex size-6 items-center justify-center rounded hover:bg-muted hover:text-foreground"
                  onClick={(event) => {
                    event.stopPropagation();
                    openBlockMenu();
                  }}
                  type="button"
                >
                  <MoreHorizontal className="size-3.5" />
                </button>
              </div>
            </DragHandle>
          ) : null}
          {blockMenuAnchor && blockMenuNode
            ? createPortal(
                <div
                  className="fixed z-[100]"
                  data-article-block-menu-anchor
                  onKeyDown={(event) => {
                    if (event.key === "Escape") {
                      event.preventDefault();
                      closeBlockMenu();
                      editor.view.focus();
                    }
                  }}
                  style={{
                    left: blockMenuAnchor.left,
                    top: blockMenuAnchor.top,
                  }}
                >
                  <ArticleBlockMenu
                    author={String(
                      blockMenuMetadata?.provenance.reviewed_by ??
                        blockMenuMetadata?.provenance.agent_id ??
                        blockMenuMetadata?.provenance.user_id ??
                        (blockMenuMetadata?.tag?.startsWith("ai_")
                          ? "AI"
                          : "Human"),
                    )}
                    blockId={blockMenuId}
                    canMoveDown={
                      blockMenuIndex >= 0 &&
                      blockMenuIndex < editor.state.doc.childCount - 1
                    }
                    canMoveUp={blockMenuIndex > 0}
                    canReview={Boolean(blockMenuMetadata)}
                    onAction={runBlockAction}
                    onClose={closeBlockMenu}
                    onCopyId={copyBlockId}
                    onCut={cutBlock}
                    onDelete={deleteBlockFromMenu}
                    onReview={reviewBlockFromMenu}
                    reviewed={blockMenuMetadata?.tag === "reviewed"}
                    updatedAt={blockMenuMetadata?.updated_at}
                  />
                </div>,
                document.body,
              )
            : null}
          {mathEditorTarget && mathEditorAnchor
            ? createPortal(
                <ArticleMathEditor
                  kind={mathEditorTarget.kind}
                  latex={mathDraft}
                  left={mathEditorAnchor.left}
                  onChange={setMathDraft}
                  onClose={closeMathEditor}
                  onDelete={deleteMathEditor}
                  onSave={saveMathEditor}
                  placement={mathEditorAnchor.placement}
                  top={mathEditorAnchor.top}
                />,
                document.body,
              )
            : null}
          {tableEdgeHandle
            ? createPortal(
                <ArticleTableEdgeControls
                  handle={tableEdgeHandle}
                  menuOpen={tableEdgeMenuOpen}
                  onAction={runTableEdgeAction}
                  onClose={() => {
                    setTableEdgeMenuOpen(false);
                    setTableEdgeHandle(undefined);
                  }}
                  onPointerDown={startTableEdgeDrag}
                  onToggleMenu={toggleTableEdgeMenu}
                />,
                document.body,
              )
            : null}
          {tableDragIndicator
            ? createPortal(
                <div
                  aria-hidden="true"
                  className={`pointer-events-none fixed z-[110] rounded-full bg-primary shadow-[0_0_0_1px_white] ${tableDragIndicator.axis === "row" ? "h-0.5" : "w-0.5"}`}
                  data-article-table-drop-indicator
                  style={{
                    height:
                      tableDragIndicator.axis === "column"
                        ? tableDragIndicator.length
                        : undefined,
                    left: tableDragIndicator.left,
                    top: tableDragIndicator.top,
                    width:
                      tableDragIndicator.axis === "row"
                        ? tableDragIndicator.length
                        : undefined,
                  }}
                />,
                document.body,
              )
            : null}
          {hoverMenu
            ? createPortal(
                <div
                  className="fixed z-[90]"
                  data-article-node-menu
                  style={{ left: hoverMenu.left, top: hoverMenu.top }}
                >
                  <button
                    aria-label={
                      hoverMenu.kind === "image"
                        ? "打开图片操作"
                        : hoverMenu.kind === "imageGroup"
                          ? "打开图片组合操作"
                          : "打开表格操作"
                    }
                    className="flex size-7 items-center justify-center rounded border bg-background text-muted-foreground shadow-sm hover:text-foreground"
                    onClick={openCurrentMenu}
                    type="button"
                  >
                    <MoreHorizontal className="size-4" />
                  </button>
                  {menuOpen ? (
                    <ArticleNodeMenu
                      alignment={currentAlignment()}
                      alt={altDraft}
                      caption={captionDraft}
                      groupColumns={currentImageGroupColumns()}
                      imageGroupContext={currentImageGroupContext()}
                      kind={hoverMenu.kind}
                      onAlign={alignImage}
                      onAltChange={setAltDraft}
                      onApplyAlt={saveAlt}
                      onApplyCaption={() => saveCaption(hoverMenu.kind)}
                      onCaptionChange={setCaptionDraft}
                      onDelete={deleteImage}
                      onDownload={() => void openImageSource(true)}
                      onGroupColumnsChange={setImageGroupColumns}
                      onImageGroupAction={runImageGroupAction}
                      onOpenSource={() => void openImageSource(false)}
                      onReplace={replaceImage}
                      onReplaceFile={replaceImageFromFile}
                      onReplaceUrlChange={setReplaceDraft}
                      onTableAction={runTableCommand}
                      replaceUrl={replaceDraft}
                      width={currentImageWidth()}
                      onWidthChange={resizeImage}
                      placement={hoverMenu.placement}
                    />
                  ) : null}
                </div>,
                document.body,
              )
            : null}
          <EditorContent
            className={`${themeClass} h-full [&_.ProseMirror]:min-h-full [&_.ProseMirror]:p-8 [&_.ProseMirror]:text-[15px] [&_.ProseMirror]:leading-7 [&_.ProseMirror]:outline-none [&_.ProseMirror_blockquote]:border-l-2 [&_.ProseMirror_blockquote]:pl-4 [&_.ProseMirror_h1]:mb-4 [&_.ProseMirror_h1]:text-3xl [&_.ProseMirror_h1]:font-bold [&_.ProseMirror_h2]:mb-3 [&_.ProseMirror_h2]:mt-8 [&_.ProseMirror_h2]:text-2xl [&_.ProseMirror_h2]:font-semibold [&_.ProseMirror_p]:my-3 [&_.ProseMirror_table]:my-4 [&_.ProseMirror_table]:w-full [&_.ProseMirror_table]:border-collapse [&_.ProseMirror_td]:border [&_.ProseMirror_td]:p-2 [&_.ProseMirror_th]:border [&_.ProseMirror_th]:bg-muted [&_.ProseMirror_th]:p-2 [&_.ProseMirror-selectednode]:rounded-md [&_.ProseMirror-selectednode]:bg-muted/60 [&_.ProseMirror-selectednode]:shadow-[inset_0_0_0_1px_hsl(var(--border))]`}
            editor={editor}
          />
        </div>
        <ArticleTagRail
          blocks={blocks}
          canEdit={canEdit}
          chapterTags={chapterTags}
          onLocate={locateBlock}
          onReview={reviewCurrentBlock}
          onReviewChapter={onReviewChapter}
          positions={tagPositions}
        />
      </div>
    </div>
  );
}

function EditorButton({
  children,
  label,
  onClick,
}: Readonly<{ children: ReactNode; label: string; onClick: () => void }>) {
  return (
    <Button aria-label={label} onClick={onClick} size="icon" variant="ghost">
      <span className="[&>svg]:size-4">{children}</span>
    </Button>
  );
}
