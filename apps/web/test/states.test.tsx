import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { EmptyState } from "@/components/states/empty-state";
import { ErrorState } from "@/components/states/error-state";
import { LoadingState } from "@/components/states/loading-state";

describe("standard page states", () => {
  it("exposes loading status to assistive technology", () => {
    render(<LoadingState label="正在读取项目" />);
    expect(screen.getByText("正在读取项目")).toBeInTheDocument();
    expect(screen.getByText("正在读取项目").parentElement).toHaveAttribute(
      "aria-busy",
      "true",
    );
  });

  it("renders descriptive empty and error states", () => {
    const { rerender } = render(
      <EmptyState description="尚未创建内容" title="空项目" />,
    );
    expect(screen.getByRole("heading", { name: "空项目" })).toBeInTheDocument();

    rerender(<ErrorState description="网络不可用" title="加载失败" />);
    expect(screen.getByRole("alert")).toHaveTextContent("网络不可用");
  });
});
