import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ArticleMathEditor } from "@/features/article/article-math-editor";

describe("ArticleMathEditor", () => {
  it("edits, saves, and deletes a formula", () => {
    const onChange = vi.fn();
    const onDelete = vi.fn();
    const onSave = vi.fn();
    render(
      <ArticleMathEditor
        kind="block"
        latex="x^2"
        left={100}
        onChange={onChange}
        onClose={vi.fn()}
        onDelete={onDelete}
        onSave={onSave}
        placement="below"
        top={100}
      />,
    );

    fireEvent.change(screen.getByLabelText("LaTeX 公式"), {
      target: { value: "x^3" },
    });
    fireEvent.keyDown(screen.getByRole("dialog"), {
      ctrlKey: true,
      key: "Enter",
    });
    fireEvent.click(screen.getByRole("button", { name: "删除公式" }));
    expect(onChange).toHaveBeenCalledWith("x^3");
    expect(onSave).toHaveBeenCalledOnce();
    expect(onDelete).toHaveBeenCalledOnce();
  });
});
