import { describe, expect, it } from "vitest";

import { TokenAuthenticator } from "../src/auth/token-authenticator.js";
import { GatewayError } from "../src/errors/gateway-error.js";
import { SessionRegistry } from "../src/sessions/session-registry.js";
import { agentToken, cliToken, testConfig } from "./helpers.js";

describe("token authentication", () => {
  const authenticator = TokenAuthenticator.fromConfig(testConfig);

  it("distinguishes CLI and Agent principals without exposing tokens", () => {
    const cli = authenticator.authenticate(`Bearer ${cliToken}`);
    const agent = authenticator.authenticate(`Bearer ${agentToken}`);

    expect(cli.principal.kind).toBe("cli");
    expect(agent.principal.kind).toBe("agent");
    expect(cli.principal.id).not.toContain(cliToken);
    expect(agent.principal.projects).toEqual(["allowed-project"]);
  });

  it("rejects missing and invalid bearer tokens", () => {
    expect(() => authenticator.authenticate(null)).toThrow(GatewayError);
    expect(() => authenticator.authenticate("Bearer invalid")).toThrow(
      GatewayError,
    );
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
