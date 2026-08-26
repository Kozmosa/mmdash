import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { ArticleAggregateWarnings } from "@/features/article/article-aggregate-warnings";

afterEach(cleanup);

describe("ArticleAggregateWarnings", () => {
  it("keeps the page usable while naming each degraded component", () => {
    render(
      <ArticleAggregateWarnings
        warnings={[
          {
            code: "ARTICLE_COMPONENT_UNAVAILABLE",
            component: "builds",
            message: "该论文区域暂时不可用；草稿仍可继续编辑，请稍后重试。",
          },
          {
            code: "ARTICLE_COMPONENT_UNAVAILABLE",
            component: "templates.bootstrap",
            message: "该论文区域暂时不可用；草稿仍可继续编辑，请稍后重试。",
          },
        ]}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "论文已以降级模式打开",
    );
    expect(screen.getByRole("status")).toHaveTextContent("构建记录");
    expect(screen.getByRole("status")).toHaveTextContent("默认模板初始化");
  });

  it("renders nothing when every component is available", () => {
    const { container } = render(<ArticleAggregateWarnings warnings={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
