import type { CoreClient } from "@mmdash/core-client";
import { describe, expect, it, vi } from "vitest";

import { TokenAuthenticator } from "../src/auth/token-authenticator.js";
import { loadConfig } from "../src/config.js";
import { GatewayError } from "../src/errors/gateway-error.js";
import { SessionRegistry } from "../src/sessions/session-registry.js";
import { agentToken, cliToken, testConfig } from "./helpers.js";

describe("token authentication", () => {
  const authenticator = TokenAuthenticator.fromConfig(testConfig);

  it("distinguishes CLI and Agent principals without exposing tokens", async () => {
    const cli = await authenticator.authenticate(
      `Bearer ${cliToken}`,
      "request-1",
    );
    const agent = await authenticator.authenticate(
      `Bearer ${agentToken}`,
      "request-2",
    );

    expect(cli.principal.kind).toBe("cli");
    expect(agent.principal.kind).toBe("agent");
    expect(cli.principal.id).not.toContain(cliToken);
    expect(agent.principal.projects).toEqual(["allowed-project"]);
  });

  it("rejects missing and invalid bearer tokens", async () => {
    await expect(
      authenticator.authenticate(null, "request-1"),
    ).rejects.toBeInstanceOf(GatewayError);
    await expect(
      authenticator.authenticate("Bearer invalid", "request-2"),
    ).rejects.toBeInstanceOf(GatewayError);
  });

  it("delegates user session validation to Core", async () => {
    const currentIdentity = vi.fn().mockResolvedValue({
      kind: "session",
      user: { id: "00000000-0000-4000-8000-000000000001" },
    });
    const delegated = TokenAuthenticator.fromConfig(testConfig, {
      currentIdentity,
    } as unknown as CoreClient);

    const result = await delegated.authenticate(
      "Bearer user-session-jwt",
      "request-3",
    );

    expect(result.principal).toMatchObject({
      delegated: true,
      id: "cli:00000000-0000-4000-8000-000000000001",
      kind: "cli",
      projects: ["*"],
    });
    expect(currentIdentity).toHaveBeenCalledWith({
      accessToken: "user-session-jwt",
      requestId: "request-3",
    });
  });

  it("distinguishes an unavailable Core auth boundary from a bad token", async () => {
    const delegated = TokenAuthenticator.fromConfig(testConfig, {
      currentIdentity: vi.fn().mockRejectedValue(new TypeError("fetch failed")),
    } as unknown as CoreClient);

    await expect(
      delegated.authenticate("Bearer user-session-jwt", "request-4"),
    ).rejects.toMatchObject({
      code: "AUTH_SERVICE_UNAVAILABLE",
      status: 503,
    });
  });
});

describe("gateway sessions", () => {
  it("renews matching sessions and rejects principal reuse", () => {
    let now = 1_000;
    const sessions = new SessionRegistry(60_000, () => now);
    const created = sessions.resolve(null, "cli:one");
    now += 1_000;
    const renewed = sessions.resolve(created.id, "cli:one");

    expect(renewed.id).toBe(created.id);
    expect(renewed.requestCount).toBe(2);
    expect(() => sessions.resolve(created.id, "agent:two")).toThrowError(
      expect.objectContaining({ code: "SESSION_PRINCIPAL_MISMATCH" }),
    );
  });

  it("expires sessions and supports explicit termination", () => {
    let now = 1_000;
    const sessions = new SessionRegistry(60_000, () => now);
    const created = sessions.resolve(null, "cli:one");
    expect(sessions.terminate(created.id, "cli:one")).toBe(true);
    expect(() => sessions.resolve(created.id, "cli:one")).toThrowError(
      expect.objectContaining({ code: "SESSION_NOT_FOUND" }),
    );

    const expiring = sessions.resolve(null, "cli:one");
    now += 60_001;
    expect(() => sessions.resolve(expiring.id, "cli:one")).toThrowError(
      expect.objectContaining({ code: "SESSION_NOT_FOUND" }),
    );
  });
});

describe("gateway configuration", () => {
  it("uses delegated Core authentication without requiring static production tokens", () => {
    const config = loadConfig({ NODE_ENV: "production" });

    expect(config.cliToken).toBeUndefined();
    expect(config.agentToken).toBeUndefined();
    expect(config.cliTools).toEqual(["*"]);
  });
});
