import path from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertPathWithin,
  createIsolatedEnvironment,
  createLayout,
  createServiceConfiguration,
  dockerAccessibleUrl,
  parseDotEnv,
  resolvePorts,
  resolveWorkerMode,
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

  it("defaults the Go module proxy to a mirror and honors overrides", () => {
    const root = path.resolve("C:/workspace/mmdash");
    const layout = createLayout(root);
    expect(
      createIsolatedEnvironment(layout, { PATH: "tools" }).GOPROXY,
    ).toContain("goproxy.cn");
    expect(
      createIsolatedEnvironment(layout, {
        PATH: "tools",
        GOPROXY: "https://proxy.golang.org,direct",
      }).GOPROXY,
    ).toBe("https://proxy.golang.org,direct");
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
    expect(
      configuration.environments.core.NOTIFICATION_WEBHOOK_ALLOW_HTTP_LOOPBACK,
    ).toBe("true");
    expect(configuration.environments.core).toMatchObject({
      AGENT_MCP_GATEWAY_URL: "http://127.0.0.1:13002/mcp",
      ARTIFACT_STORAGE_BACKEND: "minio",
      ARTIFACT_WEB_ORIGIN: "http://127.0.0.1:13000",
      CORE_INTERNAL_URL: "http://127.0.0.1:18080",
      MMDASH_PUBLIC_URL: "http://127.0.0.1:13000",
      OBJECT_STORAGE_PUBLIC_ENDPOINT: "http://127.0.0.1:19000",
      OBJECT_STORAGE_REGION: "us-east-1",
    });
    expect(configuration.environments.core.NOTION_OAUTH_REDIRECT_URI).toBe(
      "http://127.0.0.1:13000/api/integrations/notion/oauth/callback",
    );
    expect(configuration.environments.core.REPO_LOCAL_ALLOWED_ROOTS).toBe(
      layout.localRepositoryRoot,
    );
    expect(configuration.environments.core.REPO_ASKPASS_PATH).toContain(
      "mmdash-git-askpass",
    );
    expect(configuration.environments.webBff.BFF_HOST).toBe("127.0.0.1");
    expect(configuration.environments.mcp.MCP_GATEWAY_HOST).toBe("127.0.0.1");
    expect(configuration.environments.web).toMatchObject({
      BFF_INTERNAL_URL: "http://127.0.0.1:13001",
      CORE_INTERNAL_URL: "http://127.0.0.1:18080",
      MCP_INTERNAL_URL: "http://127.0.0.1:13002",
      MMDASH_LOCAL_UNIFIED_PROXY: "true",
    });
    expect(serviceOrder).toEqual([
      "postgres",
      "minio",
      "core",
      "worker",
      "web-bff",
      "mcp-gateway",
      "web",
    ]);
  });

  it("binds Core and MinIO for a container Worker without changing public URLs", () => {
    const layout = createLayout(path.resolve("C:/workspace/mmdash"));
    const configuration = createServiceConfiguration(
      resolvePorts({}),
      layout,
      {},
      "docker",
    );

    expect(configuration.coreBindHost).toBe("0.0.0.0");
    expect(configuration.minioBindHost).toBe("0.0.0.0");
    expect(configuration.coreUrl).toBe("http://127.0.0.1:18080");
    expect(configuration.minioUrl).toBe("http://127.0.0.1:19000");
    expect(dockerAccessibleUrl(configuration.coreUrl)).toBe(
      "http://host.docker.internal:18080",
    );
  });

  it("loads dotenv syntax without overriding process-level values", () => {
    expect(
      parseDotEnv(
        [
          "# comment",
          "PLAIN=value",
          'QUOTED="two words"',
          "export SINGLE='three words'",
        ].join("\n"),
      ),
    ).toEqual({
      PLAIN: "value",
      QUOTED: "two words",
      SINGLE: "three words",
    });
    expect(() => parseDotEnv("NOT VALID")).toThrow("Invalid .env entry");
  });

  it("selects a complete Worker runtime and supports an explicit base-only mode", async () => {
    await expect(resolveWorkerMode({}, async () => true)).resolves.toBe(
      "native",
    );
    await expect(
      resolveWorkerMode({}, async (command) => command !== "latexmk"),
    ).resolves.toBe("docker");
    await expect(
      resolveWorkerMode({ MMDASH_TESTENV_WORKER_MODE: "disabled" }),
    ).resolves.toBe("disabled");
    await expect(
      resolveWorkerMode({ MMDASH_TESTENV_WORKER_MODE: "invalid" }),
    ).rejects.toThrow("must be auto, native, docker, or disabled");
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
