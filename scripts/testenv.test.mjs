import path from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertPathWithin,
  createIsolatedEnvironment,
  createLayout,
  createServiceConfiguration,
  resolvePorts,
  serviceOrder,
} from "./testenv.mjs";

describe("isolated Pixi development environment", () => {
  it("keeps every writable environment path below .testenv", () => {
    const root = path.resolve("C:/workspace/mmdash");
    const layout = createLayout(root);
    const environment = createIsolatedEnvironment(layout, { PATH: "tools" });

    for (const name of [
      "APPDATA",
      "COREPACK_HOME",
      "GOCACHE",
      "GOMODCACHE",
      "GOPATH",
      "LOCALAPPDATA",
      "NPM_CONFIG_CACHE",
      "NPM_CONFIG_USERCONFIG",
      "PIXI_CACHE_DIR",
      "PIXI_HOME",
      "PNPM_HOME",
      "RATTLER_CACHE_DIR",
      "TEMP",
      "UV_CACHE_DIR",
      "UV_PROJECT_ENVIRONMENT",
      "XDG_CACHE_HOME",
      "XDG_CONFIG_HOME",
      "XDG_DATA_HOME",
      "XDG_STATE_HOME",
    ]) {
      expect(() =>
        assertPathWithin(layout.testenvRoot, environment[name]),
      ).not.toThrow();
    }
    expect(environment.HOME).toBeUndefined();
  });

  it("uses unique overridable ports and rejects unsafe values", () => {
    const ports = resolvePorts({
      MMDASH_TESTENV_WEB_PORT: "23000",
    });
    expect(ports.web).toBe(23_000);
    expect(new Set(Object.values(ports)).size).toBe(Object.keys(ports).length);
    expect(() =>
      resolvePorts({ MMDASH_TESTENV_WEB_PORT: "not-a-port" }),
    ).toThrow("must be an integer");
    expect(() =>
      resolvePorts({
        MMDASH_TESTENV_BFF_PORT: "13000",
        MMDASH_TESTENV_WEB_PORT: "13000",
      }),
    ).toThrow("must be unique");
  });

  it("builds loopback-only service configuration", () => {
    const layout = createLayout(path.resolve("C:/workspace/mmdash"));
    const ports = resolvePorts({});
    const configuration = createServiceConfiguration(ports, layout);

    expect(configuration.databaseUrl).toContain("127.0.0.1:15432");
    expect(configuration.coreUrl).toBe("http://127.0.0.1:18080");
    expect(configuration.environments.webBff.BFF_HOST).toBe("127.0.0.1");
    expect(configuration.environments.mcp.MCP_GATEWAY_HOST).toBe("127.0.0.1");
    expect(serviceOrder).toEqual([
      "postgres",
      "minio",
      "core",
      "web-bff",
      "mcp-gateway",
      "web",
    ]);
  });

  it("rejects paths that escape the isolated root", () => {
    const root = path.resolve("C:/workspace/mmdash/.testenv");
    expect(() =>
      assertPathWithin(root, path.resolve(root, "runtime")),
    ).not.toThrow();
    expect(() =>
      assertPathWithin(root, path.resolve(root, "../outside")),
    ).toThrow("outside the isolated environment");
  });
});
