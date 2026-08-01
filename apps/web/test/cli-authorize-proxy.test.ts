import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { config, proxy } from "@/proxy";

describe("CLI authorization route protection", () => {
  it("registers the authorization page with the Next proxy matcher", () => {
    expect(config.matcher).toContain("/cli/authorize/:path*");
  });

  it("redirects an unauthenticated device approval to login with its code", () => {
    const response = proxy(
      new NextRequest("https://mmdash.test/cli/authorize?user_code=ABCD-EFGH"),
    );

    expect(response.headers.get("location")).toBe(
      "https://mmdash.test/login?returnTo=%2Fcli%2Fauthorize%3Fuser_code%3DABCD-EFGH",
    );
  });

  it("allows a signed browser session to reach device approval", () => {
    const response = proxy(
      new NextRequest("https://mmdash.test/cli/authorize", {
        headers: { cookie: "mmdash_session=signed" },
      }),
    );

    expect(response.headers.get("x-middleware-next")).toBe("1");
  });
});
