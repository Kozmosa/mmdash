"use client";

import { Button } from "@/components/ui/button";

import {
  CUMCM_CHAPTERS,
  CUMCM_ENVIRONMENTS,
  dispatchArticleCumcmInsert,
} from "./article-cumcm";
import {
  articleOutlineNavigateEvent,
  type ArticleOutlineItem,
} from "./article-editor";

// Chapter and environment insertion for projects using the CUMCM built-in
// template. The "/" menu entries are registered project-wide by the
// workbench, independent of whether this panel is open.
export function ArticleCumcmPanel({
  canEdit,
  outline,
}: {
  canEdit: boolean;
  outline: ArticleOutlineItem[];
}) {
  const headingTexts = new Set(outline.map((item) => item.text.trim()));

  return (
    <div className="grid gap-4 text-sm">
      <section className="grid gap-2">
        <div className="flex items-center gap-2">
          <p className="text-xs font-medium">CUMCM 章节</p>
          <Button
            className="ml-auto h-7 px-2 text-xs"
            disabled={!canEdit}
            onClick={() => dispatchArticleCumcmInsert({ kind: "all", key: "" })}
            size="sm"
            variant="secondary"
          >
            插入全部章节
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          章节结构对应 cumcmthesis 模板的 texfile
          脚手架;插入后可在下方目录中点击章节定位。参考文献由 Zotero
          引用在构建时自动生成,无需手写章节。
        </p>
        <div className="grid gap-1">
          {CUMCM_CHAPTERS.map((chapter) => {
            const inserted = chapter.headings.every((value) =>
              headingTexts.has(value),
            );
            const firstPresent = outline.find((item) =>
              chapter.headings.includes(item.text.trim()),
            );
            return (
              <div className="flex items-center gap-1" key={chapter.key}>
                <span
                  className={`size-1.5 shrink-0 rounded-full ${inserted ? "bg-primary" : "bg-muted-foreground/30"}`}
                />
                <button
                  className="min-w-0 flex-1 truncate rounded px-1 py-1 text-left hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent"
                  disabled={!firstPresent}
                  onClick={() =>
                    firstPresent &&
                    window.dispatchEvent(
                      new CustomEvent(articleOutlineNavigateEvent, {
                        detail: { id: firstPresent.id },
                      }),
                    )
                  }
                  title={chapter.headings.join("、")}
                  type="button"
                >
                  {chapter.label}
                </button>
                <Button
                  className="h-6 shrink-0 px-2 text-xs"
                  disabled={!canEdit}
                  onClick={() =>
                    dispatchArticleCumcmInsert({
                      kind: "chapter",
                      key: chapter.key,
                    })
                  }
                  size="sm"
                  variant="ghost"
                >
                  插入
                </Button>
              </div>
            );
          })}
        </div>
      </section>
      <section className="grid gap-2">
        <p className="text-xs font-medium">模板环境</p>
        <p className="text-xs text-muted-foreground">
          cumcmthesis.cls 定义的定理类环境,以原文块写入,构建时原样交给
          XeLaTeX;也可以在正文输入 “/” 后按名称筛选。
        </p>
        <div className="flex flex-wrap gap-1">
          {CUMCM_ENVIRONMENTS.map((item) => (
            <Button
              className="h-7 px-2 text-xs"
              disabled={!canEdit}
              key={item.key}
              onClick={() =>
                dispatchArticleCumcmInsert({ kind: "env", key: item.key })
              }
              size="sm"
              variant="outline"
            >
              {item.label}
            </Button>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          环境编号通过 \label 引用,例如 \cref&#123;asu:1&#125;。
        </p>
      </section>
      <section className="grid gap-1">
        <p className="text-xs font-medium text-muted-foreground">
          仅国赛模板项目显示
        </p>
        <p className="text-xs text-muted-foreground">
          本面板只在项目注册了 CUMCM
          内置模板时出现,不影响其他项目的通用编辑流程。
        </p>
      </section>
    </div>
  );
}
