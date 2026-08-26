import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ZoteroCitationChip } from "@/features/article/article-zotero-citation-node-view";

afterEach(cleanup);

describe("ZoteroCitationChip", () => {
  it("opens immutable citation details and deletes the editor node", () => {
    const onDelete = vi.fn();
    render(
      <ZoteroCitationChip
        canDelete
        citationKey="Smith2026"
        itemKey="ABC123"
        onDelete={onDelete}
        referenceId="reference-1"
        title="A modeling paper"
        version={7}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "查看 Zotero 引用：A modeling paper",
      }),
    );
    expect(screen.getByRole("dialog")).toHaveTextContent(
      "Zotero item ABC123 · version 7",
    );
    expect(screen.getByRole("dialog")).toHaveTextContent(
      "固定引用 reference-1",
    );
    fireEvent.click(screen.getByRole("button", { name: "删除此引用" }));
    expect(onDelete).toHaveBeenCalledOnce();
  });

  it("keeps citation details readable for viewers without a delete action", () => {
    render(
      <ZoteroCitationChip
        canDelete={false}
        citationKey="ReadOnly2026"
        itemKey="ITEM"
        onDelete={vi.fn()}
        referenceId="reference-2"
        title="Read-only citation"
        version={2}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: "查看 Zotero 引用：Read-only citation",
      }),
    );
    expect(screen.queryByRole("button", { name: "删除此引用" })).toBeNull();
  });
});
