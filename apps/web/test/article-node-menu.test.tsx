import { fireEvent, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { describe, expect, it, vi } from "vitest";

import { ArticleNodeMenu } from "@/features/article/article-node-menu";

function imageMenu(
  overrides: Partial<Parameters<typeof ArticleNodeMenu>[0]> = {},
) {
  return render(
    createElement(ArticleNodeMenu, {
      alignment: "center",
      alt: "旧 alt",
      caption: "旧图注",
      groupColumns: 2,
      kind: "image",
      onAlign: vi.fn(),
      onAltChange: vi.fn(),
      onApplyAlt: vi.fn(),
      onApplyCaption: vi.fn(),
      onCaptionChange: vi.fn(),
      onDelete: vi.fn(),
      onDownload: vi.fn(),
      onGroupColumnsChange: vi.fn(),
      onOpenSource: vi.fn(),
      onReplace: vi.fn(),
      onReplaceFile: vi.fn(),
      onReplaceUrlChange: vi.fn(),
      onTableAction: vi.fn(),
      replaceUrl: "https://example.com/old.png",
      width: 80,
      onWidthChange: vi.fn(),
      ...overrides,
    }),
  );
}

describe("Article node menus", () => {
  it("exposes image caption, alt, replace, alignment, and delete actions", () => {
    const onAlign = vi.fn();
    const onApplyAlt = vi.fn();
    const onApplyCaption = vi.fn();
    const onDelete = vi.fn();
    const onReplace = vi.fn();
    const onReplaceFile = vi.fn();
    const onDownload = vi.fn();
    const onOpenSource = vi.fn();
    const onWidthChange = vi.fn();
    imageMenu({
      onAlign,
      onApplyAlt,
      onApplyCaption,
      onDelete,
      onDownload,
      onOpenSource,
      onReplace,
      onReplaceFile,
      onWidthChange,
    });

    fireEvent.change(screen.getByLabelText("图注（图片下方）"), {
      target: { value: "图 1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存图注" }));
    fireEvent.click(screen.getByRole("button", { name: "保存替代文本" }));
    fireEvent.click(screen.getByRole("button", { name: "图片右对齐" }));
    fireEvent.click(screen.getByRole("button", { name: "替换图片" }));
    fireEvent.click(screen.getByRole("button", { name: "上传本地图片替换" }));
    fireEvent.change(screen.getByLabelText("图片宽度"), {
      target: { value: "65" },
    });
    fireEvent.click(screen.getByRole("button", { name: "打开源文件" }));
    fireEvent.click(screen.getByRole("button", { name: "下载原图" }));
    fireEvent.click(screen.getByRole("button", { name: "删除图片" }));

    expect(onApplyCaption).toHaveBeenCalledOnce();
    expect(onApplyAlt).toHaveBeenCalledOnce();
    expect(onAlign).toHaveBeenCalledWith("right");
    expect(onReplace).toHaveBeenCalledOnce();
    expect(onReplaceFile).toHaveBeenCalledOnce();
    expect(onWidthChange).toHaveBeenCalledWith(65);
    expect(onOpenSource).toHaveBeenCalledOnce();
    expect(onDownload).toHaveBeenCalledOnce();
    expect(onDelete).toHaveBeenCalledOnce();
  });

  it("keeps row and column actions on edge handles, not the caption menu", () => {
    const onApplyCaption = vi.fn();
    const onTableAction = vi.fn();
    render(
      createElement(ArticleNodeMenu, {
        alignment: "center",
        alt: "",
        caption: "表注",
        groupColumns: 2,
        kind: "table",
        onAlign: vi.fn(),
        onAltChange: vi.fn(),
        onApplyAlt: vi.fn(),
        onApplyCaption,
        onCaptionChange: vi.fn(),
        onDelete: vi.fn(),
        onDownload: vi.fn(),
        onGroupColumnsChange: vi.fn(),
        onOpenSource: vi.fn(),
        onReplace: vi.fn(),
        onReplaceFile: vi.fn(),
        onReplaceUrlChange: vi.fn(),
        onTableAction,
        replaceUrl: "",
        width: 100,
        onWidthChange: vi.fn(),
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "保存表注" }));
    fireEvent.click(screen.getByRole("button", { name: "切换首行为表头" }));
    fireEvent.click(screen.getByRole("button", { name: "删除表格" }));

    expect(onApplyCaption).toHaveBeenCalledOnce();
    expect(onTableAction.mock.calls.map(([action]) => action)).toEqual([
      "toggleHeaderRow",
      "deleteTable",
    ]);
    expect(screen.queryByRole("button", { name: "上方插入行" })).toBeNull();
    expect(screen.queryByRole("button", { name: "左侧插入列" })).toBeNull();
  });

  it("configures a wrapping image group and keeps its large caption", () => {
    const onApplyCaption = vi.fn();
    const onGroupColumnsChange = vi.fn();
    const onImageGroupAction = vi.fn();
    imageMenu({
      caption: "组合结果",
      groupColumns: 3,
      kind: "imageGroup",
      onApplyCaption,
      onGroupColumnsChange,
      onImageGroupAction,
    });

    fireEvent.click(screen.getByRole("button", { name: "保存组合大题注" }));
    fireEvent.click(screen.getByRole("button", { name: "每行 4 张图片" }));
    fireEvent.click(screen.getByRole("button", { name: "拆分为独立图片" }));

    expect(onApplyCaption).toHaveBeenCalledOnce();
    expect(onGroupColumnsChange).toHaveBeenCalledWith(4);
    expect(onImageGroupAction).toHaveBeenCalledWith("ungroup");
  });

  it("offers adjacent-image grouping and removal without hiding image settings", () => {
    const onImageGroupAction = vi.fn();
    const view = imageMenu({
      imageGroupContext: {
        canMergeAfter: true,
        canMergeBefore: false,
        inGroup: false,
      },
      onImageGroupAction,
    });
    fireEvent.click(screen.getByRole("button", { name: "与下一张图片组合" }));
    expect(onImageGroupAction).toHaveBeenCalledWith("mergeAfter");

    view.unmount();
    imageMenu({
      imageGroupContext: {
        canMergeAfter: false,
        canMergeBefore: false,
        inGroup: true,
      },
      onImageGroupAction,
    });
    fireEvent.click(screen.getByRole("button", { name: "移出图片组合" }));
    expect(onImageGroupAction).toHaveBeenCalledWith("removeFromGroup");
  });
});
