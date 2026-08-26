import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const projectId = "00000000-0000-4000-8000-000000000001";
const artifactId = "00000000-0000-4000-8000-000000000002";
const uploadId = "00000000-0000-4000-8000-000000000003";
const localToken = `${"a".repeat(80)}.${"b".repeat(43)}`;

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Artifact BFF routes", () => {
  it("validates filters and rewrites only Core-signed transfer grants", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockImplementation((input) => {
        const url = String(input);
        if (url.endsWith("/parts/sign")) {
          return Promise.resolve(
            Response.json({
              items: [
                {
                  part_number: 1,
                  size_bytes: 5,
                  transfer: {
                    expires_at: "2026-07-30T00:01:00Z",
                    headers: {},
                    method: "PUT",
                    url: `http://localhost:3000/v1/artifact-transfers/${localToken}`,
                  },
                },
                {
                  part_number: 2,
                  size_bytes: 5,
                  transfer: {
                    expires_at: "2026-07-30T00:01:00Z",
                    headers: { "x-amz-checksum-sha256": "checksum" },
                    method: "PUT",
                    url: "http://minio.test/bucket/key?X-Amz-Signature=signed",
                  },
                },
              ],
            }),
          );
        }
        return Promise.resolve(
          Response.json({ has_more: false, items: [], next_cursor: null }),
        );
      });
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const invalid = await app.inject({
      headers: { cookie },
      method: "GET",
      url: `/api/projects/${projectId}/artifacts?kind=untrusted`,
    });
    expect(invalid.statusCode).toBe(400);
    expect(fetchImplementation).not.toHaveBeenCalled();

    const list = await app.inject({
      headers: { cookie },
      method: "GET",
      url: `/api/projects/${projectId}/artifacts?kind=problem&source=user_upload&status=available&tag=source`,
    });
    expect(list.statusCode).toBe(200);
    expect(String(fetchImplementation.mock.calls[0]?.[0])).toBe(
      `http://core.test/v1/projects/${projectId}/artifacts?kind=problem&source=user_upload&status=available&tag=source`,
    );

    const signed = await app.inject({
      headers: { cookie },
      method: "POST",
      payload: { part_numbers: [1, 2] },
      url: `/api/projects/${projectId}/artifacts/uploads/${uploadId}/parts/sign`,
    });
    expect(signed.statusCode).toBe(200);
    expect(signed.json()).toMatchObject({
      items: [
        {
          transfer: { url: `/api/artifact-transfers/${localToken}` },
        },
        {
          transfer: {
            url: "http://minio.test/bucket/key?X-Amz-Signature=signed",
          },
        },
      ],
    });
  });

  it("streams a signed Local part without buffering or project headers", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(null, {
        headers: { etag: '"local-etag"' },
        status: 204,
      }),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: {
        "content-length": "5",
        "content-type": "application/octet-stream",
        cookie,
      },
      method: "PUT",
      payload: Buffer.from("hello"),
      url: `/api/artifact-transfers/${localToken}`,
    });

    expect(response.statusCode).toBe(204);
    expect(response.headers.etag).toBe('"local-etag"');
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(`http://core.test/v1/artifact-transfers/${localToken}`);
    expect(options?.method).toBe("PUT");
    expect(options?.duplex).toBe("half");
    const headers = new Headers(options?.headers);
    expect(headers.get("content-length")).toBe("5");
    expect(headers.get("authorization")).toBe("Bearer test-access-token");
    expect(headers.get("x-mmdash-project-id")).toBeNull();
  });

  it("keeps trash, restore, purge, and version download project-scoped", async () => {
    const versionId = "00000000-0000-4000-8000-000000000004";
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockImplementation((input, options) => {
        if (String(input).endsWith("/download")) {
          return Promise.resolve(
            Response.json({
              artifact_id: artifactId,
              filename: "problem.pdf",
              mime_type: "application/pdf",
              size_bytes: 42,
              transfer: {
                expires_at: "2026-07-30T00:01:00Z",
                headers: {},
                method: "GET",
                url: `http://localhost:3000/v1/artifact-transfers/${localToken}`,
              },
              version_id: versionId,
            }),
          );
        }
        return Promise.resolve(
          options?.method === "DELETE"
            ? new Response(null, { status: 204 })
            : Response.json({ artifact: { artifact_id: artifactId } }),
        );
      });
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const download = await app.inject({
      headers: { cookie },
      method: "POST",
      url: `/api/projects/${projectId}/artifacts/${artifactId}/versions/${versionId}/download`,
    });
    expect(download.json()).toMatchObject({
      transfer: { url: `/api/artifact-transfers/${localToken}` },
    });

    for (const request of [
      {
        method: "DELETE" as const,
        url: `/api/projects/${projectId}/artifacts/${artifactId}`,
      },
      {
        method: "POST" as const,
        url: `/api/projects/${projectId}/artifacts/${artifactId}/restore`,
      },
      {
        method: "DELETE" as const,
        url: `/api/projects/${projectId}/artifacts/${artifactId}/purge`,
      },
    ]) {
      const response = await app.inject({ headers: { cookie }, ...request });
      expect([200, 204]).toContain(response.statusCode);
    }
    for (const call of fetchImplementation.mock.calls) {
      const headers = new Headers(call[1]?.headers);
      expect(headers.get("x-mmdash-project-id")).toBe(projectId);
    }
  });

  it("proxies the durable folder tree and folder assignments", async () => {
    const folderId = "00000000-0000-4000-8000-000000000005";
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockImplementation((input, options) => {
        const url = String(input);
        if (url.endsWith("/artifacts/folders") && options?.method === "GET") {
          return Promise.resolve(
            Response.json({
              items: [
                {
                  children: [],
                  folder_id: folderId,
                  name: "Data",
                  parent_folder_id: null,
                  position: 0,
                  project_id: projectId,
                },
              ],
            }),
          );
        }
        if (
          url.endsWith(`/artifacts/folders/${folderId}?recursive=true`) &&
          options?.method === "DELETE"
        ) {
          return Promise.resolve(new Response(null, { status: 204 }));
        }
        return Promise.resolve(
          Response.json({
            artifact: { artifact_id: artifactId, folder_id: folderId },
          }),
        );
      });
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const tree = await app.inject({
      headers: { cookie },
      method: "GET",
      url: `/api/projects/${projectId}/artifacts/folders`,
    });
    expect(tree.statusCode).toBe(200);
    expect(tree.json().items[0].name).toBe("Data");

    const moved = await app.inject({
      headers: { cookie },
      method: "PUT",
      payload: { folder_id: folderId },
      url: `/api/projects/${projectId}/artifacts/${artifactId}/folder`,
    });
    expect(moved.statusCode).toBe(200);
    expect(moved.json().artifact.folder_id).toBe(folderId);
    const [url, options] = fetchImplementation.mock.calls.at(-1)!;
    expect(url).toBe(
      `http://core.test/v1/projects/${projectId}/artifacts/${artifactId}/folder`,
    );
    expect(options?.method).toBe("PUT");

    const deleted = await app.inject({
      headers: { cookie },
      method: "DELETE",
      url: `/api/projects/${projectId}/artifacts/folders/${folderId}?recursive=true`,
    });
    expect(deleted.statusCode).toBe(204);
    const [deleteUrl, deleteOptions] = fetchImplementation.mock.calls.at(-1)!;
    expect(deleteUrl).toBe(
      `http://core.test/v1/projects/${projectId}/artifacts/folders/${folderId}?recursive=true`,
    );
    expect(deleteOptions?.method).toBe("DELETE");
  });
});
