import { CoreClientError, type CoreClient } from "@mmdash/core-client";
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

  it("maps active product Agent credentials to a stable exact grant", async () => {
    const currentIdentity = vi.fn().mockResolvedValue({
      agent_instance_id: "00000000-0000-4000-8000-000000000021",
      allowed_tools: [
        "project.get",
        "data.list",
        "data.read",
        "context.promote",
      ],
      credential_status: "active",
      kind: "agent",
      project_id: "00000000-0000-4000-8000-000000000011",
      token_id: "00000000-0000-4000-8000-000000000031",
    });
    const delegated = TokenAuthenticator.fromConfig(testConfig, {
      currentIdentity,
    } as unknown as CoreClient);

    const first = await delegated.authenticate(
      "Bearer product-agent-token-one",
      "request-agent-1",
    );
    const rotated = await delegated.authenticate(
      "Bearer product-agent-token-two",
      "request-agent-2",
    );

    expect(first.principal).toEqual({
      agentInstanceId: "00000000-0000-4000-8000-000000000021",
      credentialStatus: "active",
      delegated: true,
      id: "agent:00000000-0000-4000-8000-000000000021",
      kind: "agent",
      projects: ["00000000-0000-4000-8000-000000000011"],
      sessionOwnerId:
        "agent:00000000-0000-4000-8000-000000000021:credential:00000000-0000-4000-8000-000000000031",
      tokenId: "00000000-0000-4000-8000-000000000031",
      tools: ["project.get", "data.list", "data.read", "context.promote"],
    });
    expect(rotated.principal.id).toBe(first.principal.id);
  });

  it("retains the exact reviewed grant while marking pending credentials", async () => {
    const activeAgent = {
      agent_instance_id: "00000000-0000-4000-8000-000000000021",
      allowed_tools: ["data.read"],
      credential_status: "active",
      kind: "agent",
      project_id: "00000000-0000-4000-8000-000000000011",
      token_id: "00000000-0000-4000-8000-000000000031",
    };
    const currentIdentity = vi.fn().mockResolvedValue({
      ...activeAgent,
      credential_status: "pending",
    });
    const delegated = TokenAuthenticator.fromConfig(testConfig, {
      currentIdentity,
    } as unknown as CoreClient);

    const result = await delegated.authenticate(
      "Bearer pending-agent-token",
      "request-pending",
    );

    expect(result.principal).toMatchObject({
      agentInstanceId: activeAgent.agent_instance_id,
      credentialStatus: "pending",
      kind: "agent",
      projects: [activeAgent.project_id],
      sessionOwnerId:
        "agent:00000000-0000-4000-8000-000000000021:credential:00000000-0000-4000-8000-000000000031",
      tokenId: activeAgent.token_id,
      tools: ["data.read"],
    });
  });

  it("rejects wildcard, malformed, inactive, and unknown delegated identities", async () => {
    const activeAgent = {
      agent_instance_id: "00000000-0000-4000-8000-000000000021",
      allowed_tools: ["data.read"],
      credential_status: "active",
      kind: "agent",
      project_id: "00000000-0000-4000-8000-000000000011",
      token_id: "00000000-0000-4000-8000-000000000031",
    };
    const currentIdentity = vi
      .fn()
      .mockResolvedValueOnce({ ...activeAgent, allowed_tools: ["*"] })
      .mockResolvedValueOnce({
        ...activeAgent,
        allowed_tools: ["data.*"],
      })
      .mockResolvedValueOnce({ ...activeAgent, credential_status: "revoked" })
      .mockResolvedValueOnce({ kind: "box" });
    const delegated = TokenAuthenticator.fromConfig(testConfig, {
      currentIdentity,
    } as unknown as CoreClient);

    await expect(
      delegated.authenticate("Bearer wildcard-agent-token", "request-wildcard"),
    ).rejects.toMatchObject({ code: "AGENT_IDENTITY_INVALID", status: 401 });
    await expect(
      delegated.authenticate("Bearer prefix-agent-token", "request-prefix"),
    ).rejects.toMatchObject({ code: "AGENT_IDENTITY_INVALID", status: 401 });
    await expect(
      delegated.authenticate("Bearer revoked-agent-token", "request-revoked"),
    ).rejects.toMatchObject({ code: "AGENT_CREDENTIAL_INACTIVE", status: 401 });
    await expect(
      delegated.authenticate("Bearer box-token", "request-box"),
    ).rejects.toMatchObject({ code: "UNAUTHENTICATED", status: 401 });
  });

  it("rejects a revoked product Agent token without exposing it", async () => {
    const revokedToken = "revoked-product-agent-token-secret";
    const delegated = TokenAuthenticator.fromConfig(testConfig, {
      currentIdentity: vi.fn().mockRejectedValue(
        new CoreClientError(401, {
          code: "UNAUTHENTICATED",
          message: `revoked ${revokedToken}`,
        }),
      ),
    } as unknown as CoreClient);

    const rejection = delegated.authenticate(
      `Bearer ${revokedToken}`,
      "request-revoked",
    );
    await expect(rejection).rejects.toMatchObject({
      code: "UNAUTHENTICATED",
      message: "A valid user or Agent token is required",
      status: 401,
    });
    await expect(rejection).rejects.not.toHaveProperty(
      "message",
      expect.stringContaining(revokedToken),
    );
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
    expect(created.initialized).toBe(false);
    now += 1_000;
    const renewed = sessions.resolve(created.id, "cli:one");

    expect(renewed.id).toBe(created.id);
    expect(renewed.requestCount).toBe(2);
    expect(sessions.markInitialized(created.id, "cli:one").initialized).toBe(
      true,
    );
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

  it("rejects the static Agent token in production", () => {
    expect(() =>
      loadConfig({
        MCP_AGENT_TOKEN: "production-agent-token-with-at-least-32-characters",
        NODE_ENV: "production",
      }),
    ).toThrow("MCP_AGENT_TOKEN is available only for development and tests");
  });
});
