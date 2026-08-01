import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import CliAuthorizePage from "@/app/cli/authorize/page";

const { request } = vi.hoisted(() => ({ request: vi.fn() }));

vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams("user_code=ABCD-EFGH"),
}));
vi.mock("@/lib/api-client", () => ({
  apiClient: { request },
}));

describe("CLI device authorization", () => {
  beforeEach(() => request.mockReset().mockResolvedValue(undefined));

  it("approves the displayed device code through the authenticated BFF", async () => {
    render(<CliAuthorizePage />);

    fireEvent.click(screen.getByRole("button", { name: "授权" }));

    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("/auth/device/verify", {
        body: { approve: true, user_code: "ABCD-EFGH" },
        method: "POST",
      }),
    );
    expect(
      await screen.findByText("授权成功。你现在可以返回终端。"),
    ).toBeInTheDocument();
  });
});
