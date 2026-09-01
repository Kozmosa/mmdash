import { readFile } from "node:fs/promises";

import yaml from "js-yaml";
import { describe, expect, it } from "vitest";

const composePath = "deploy/production/compose.yaml";
const caddyfilePath = "deploy/production/caddy/Caddyfile";
const caddyDockerfilePath = "deploy/production/caddy/Dockerfile";
const mihomoConfigPath = "deploy/production/mihomo/config.example.yaml";
const mihomoDockerfilePath = "deploy/production/mihomo/Dockerfile";

describe("production deployment", () => {
  it("keeps every service off host ports and preserves private boundaries", async () => {
    const compose = yaml.load(await readFile(composePath, "utf8"));
    const services = compose.services;

    expect(Object.keys(services)).toEqual(
      expect.arrayContaining([
        "postgres",
        "minio",
        "minio-init",
        "migrate",
        "mihomo",
        "core",
        "web-bff",
        "web",
        "mcp-gateway",
        "worker",
        "caddy",
        "cloudflared",
      ]),
    );

    for (const service of Object.values(services)) {
      expect(service).not.toHaveProperty("ports");
      expect(service).not.toHaveProperty("network_mode", "host");
      expect(service).not.toHaveProperty("privileged", true);
    }

    expect(compose.networks.edge.internal).toBe(true);
    expect(compose.networks.app.internal).toBe(true);
    expect(compose.networks.data.internal).toBe(true);
    expect(compose.networks.egress?.internal).not.toBe(true);
    expect(compose.networks["tunnel-egress"]?.internal).not.toBe(true);

    expect(services.cloudflared.networks).toEqual(["edge", "tunnel-egress"]);
    expect(services.caddy.networks).toEqual(["edge", "app", "data"]);
    expect(services.mihomo.networks).toEqual(["egress"]);
    expect(services.core.networks).toEqual(["app", "data", "egress"]);
    expect(services.postgres.networks).toEqual(["data"]);
    expect(services.minio.networks).toEqual(["data"]);
    expect(services.worker.networks).toEqual(["data"]);
    expect(services.worker.profiles).toEqual(["worker"]);
  });

  it("runs the Repo proxy as an isolated non-root Compose service", async () => {
    const compose = yaml.load(await readFile(composePath, "utf8"));
    const { core, mihomo } = compose.services;

    expect(mihomo.image).toContain("MMDASH_MIHOMO_IMAGE");
    expect(mihomo.read_only).toBe(true);
    expect(mihomo.user).toContain("MMDASH_MIHOMO_UID");
    expect(mihomo.user).toContain("MMDASH_MIHOMO_GID");
    expect(mihomo.cap_drop).toEqual(["ALL"]);
    expect(mihomo.security_opt).toEqual(["no-new-privileges:true"]);
    expect(mihomo.restart).toBe("unless-stopped");
    expect(mihomo.volumes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ read_only: true, target: "/config.yaml" }),
        expect.objectContaining({
          read_only: true,
          target: "/state/providers",
        }),
      ]),
    );
    expect(mihomo.tmpfs).toEqual([
      expect.stringContaining("/state:rw,noexec,nosuid"),
    ]);
    expect(mihomo.healthcheck.test).toEqual(
      expect.arrayContaining(["/usr/local/bin/mihomo", "-t"]),
    );
    expect(core.environment.REPO_GITHUB_PROXY_URL).toContain(
      "REPO_GITHUB_PROXY_URL",
    );
    expect(core.depends_on.mihomo.condition).toBe("service_healthy");
    expect(compose.networks.egress).not.toHaveProperty("ipam");
    expect(compose.networks.egress).not.toHaveProperty("driver_opts");
  });

  it("makes Cloudflare Tunnel the sole ingress through Caddy", async () => {
    const compose = yaml.load(await readFile(composePath, "utf8"));
    const { caddy, cloudflared } = compose.services;

    expect(cloudflared.command).toEqual(["tunnel", "--no-autoupdate", "run"]);
    expect(cloudflared.environment.TUNNEL_TOKEN).toContain(
      "CLOUDFLARE_TUNNEL_TOKEN",
    );
    expect(cloudflared.depends_on.caddy.condition).toBe("service_healthy");
    expect(cloudflared.read_only).toBe(true);
    expect(caddy.read_only).toBe(true);
    expect(caddy.depends_on).toMatchObject({
      "mcp-gateway": { condition: "service_healthy" },
      minio: { condition: "service_healthy" },
      web: { condition: "service_healthy" },
    });
  });

  it("keeps source-build registry mirrors scoped to Compose build arguments", async () => {
    const compose = yaml.load(await readFile(composePath, "utf8"));
    const services = compose.services;

    expect(services.core.build.args).toMatchObject({
      ALPINE_BASE_IMAGE: expect.stringContaining("ALPINE_BASE_IMAGE"),
      GO_BASE_IMAGE: expect.stringContaining("GO_BASE_IMAGE"),
    });
    expect(services.web.build.args.NODE_BASE_IMAGE).toContain(
      "NODE_BASE_IMAGE",
    );
    expect(services["web-bff"].build.args.NODE_BASE_IMAGE).toContain(
      "NODE_BASE_IMAGE",
    );
    expect(services["mcp-gateway"].build.args.NODE_BASE_IMAGE).toContain(
      "NODE_BASE_IMAGE",
    );
    expect(services.worker.build.args.PYTHON_BASE_IMAGE).toContain(
      "PYTHON_BASE_IMAGE",
    );
    expect(services.caddy.build.args.CADDY_BASE_IMAGE).toContain(
      "CADDY_BASE_IMAGE",
    );
  });

  it("routes only reviewed public boundaries and never exposes Core", async () => {
    const caddyfile = await readFile(caddyfilePath, "utf8");

    for (const fragment of [
      "host {$MMDASH_PRODUCTION_HOST}",
      "path /api/*",
      "path /v1/*",
      "path /mcp /mcp/*",
      "path /{$OBJECT_STORAGE_BUCKET} /{$OBJECT_STORAGE_BUCKET}/*",
      "reverse_proxy web-bff:3001",
      "reverse_proxy mcp-gateway:3002",
      "reverse_proxy minio:9000",
      "reverse_proxy web:3000",
      "log_skip",
      "remote_ip 127.0.0.1 ::1",
      "header_up X-Forwarded-Proto https",
    ]) {
      expect(caddyfile).toContain(fragment);
    }

    expect(caddyfile).not.toContain("reverse_proxy core:8080");
    expect(caddyfile).not.toContain("reverse_proxy postgres");
    expect(caddyfile).not.toContain("reverse_proxy minio:9001");
  });

  it("runs the production Caddy image as an unprivileged read-only process", async () => {
    const dockerfile = await readFile(caddyDockerfilePath, "utf8");

    expect(dockerfile).toContain("setcap -r /usr/bin/caddy");
    expect(dockerfile).toContain("--chown=65532:65532 --chmod=0444");
    expect(dockerfile).toContain("USER 65532:65532");
    expect(dockerfile).toContain("EXPOSE 8080");
  });

  it("builds a minimal pinned mihomo image with a fail-closed config", async () => {
    const dockerfile = await readFile(mihomoDockerfilePath, "utf8");
    const config = yaml.load(await readFile(mihomoConfigPath, "utf8"));

    expect(dockerfile).toContain("FROM scratch");
    expect(dockerfile).toContain("ca-certificates.crt");
    expect(dockerfile).toContain("COPY --chmod=0555 mihomo");
    expect(dockerfile).toContain("USER 65532:65532");
    expect(config["mixed-port"]).toBe(17890);
    expect(config["bind-address"]).toBe("0.0.0.0");
    expect(config["external-controller"]).toBe("127.0.0.1:19090");
    expect(config["proxy-providers"].upstream.path).toBe(
      "/state/providers/upstream.yaml",
    );
    expect(config["proxy-groups"]).toEqual([
      expect.objectContaining({
        name: "MMDASH-REPO-EGRESS",
        type: "url-test",
        use: ["upstream"],
      }),
    ]);
    expect(config.rules).toEqual(["MATCH,MMDASH-REPO-EGRESS"]);
    expect(config.rules).not.toContain("MATCH,DIRECT");
  });
});
