import { once } from "node:events";

import { afterEach, describe, expect, it } from "vitest";

import { createContractMockServer } from "./contract-mock.mjs";

const servers = [];

afterEach(async () => {
  await Promise.all(
    servers
      .splice(0)
      .map(
        (server) =>
          new Promise((resolve, reject) =>
            server.close((error) => (error ? reject(error) : resolve())),
          ),
      ),
  );
});

describe("Core contract mock", () => {
  it("serves operation fixtures and identifies the matched operation", async () => {
    const server = await createContractMockServer();
    servers.push(server);
    server.listen(0, "127.0.0.1");
    await once(server, "listening");
    const address = server.address();
    const response = await fetch(
      `http://127.0.0.1:${address.port}/v1/projects`,
    );

    expect(response.status).toBe(200);
    expect(response.headers.get("x-mmdash-operation-id")).toBe("projects.list");
    expect(await response.json()).toEqual({ items: [] });
  });

  it("returns a stable error for an unregistered fixture", async () => {
    const server = await createContractMockServer();
    servers.push(server);
    server.listen(0, "127.0.0.1");
    await once(server, "listening");
    const address = server.address();
    const response = await fetch(`http://127.0.0.1:${address.port}/not-found`);

    expect(response.status).toBe(404);
    expect(await response.json()).toMatchObject({
      code: "MOCK_OPERATION_NOT_FOUND",
    });
  });
});
