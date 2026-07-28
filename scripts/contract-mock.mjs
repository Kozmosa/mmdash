import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

export async function loadMockFixtures() {
  return JSON.parse(
    await readFile(
      path.join(root, "contracts/mock/core-responses.json"),
      "utf8",
    ),
  );
}

export async function createContractMockServer() {
  const fixtures = Object.entries(await loadMockFixtures()).map(
    ([operationId, fixture]) => ({ operationId, ...fixture }),
  );
  return createServer((request, response) => {
    const pathname = new URL(request.url ?? "/", "http://contract.mock")
      .pathname;
    const fixture = fixtures.find(
      (candidate) =>
        candidate.method === request.method && candidate.path === pathname,
    );
    response.setHeader("content-type", "application/json");
    if (!fixture) {
      response.writeHead(404);
      response.end(
        JSON.stringify({
          code: "MOCK_OPERATION_NOT_FOUND",
          message: "No mock fixture is registered for this operation",
        }),
      );
      return;
    }
    response.setHeader("x-mmdash-operation-id", fixture.operationId);
    response.writeHead(fixture.status);
    response.end(JSON.stringify(fixture.body));
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const host = process.env.CONTRACT_MOCK_HOST ?? "127.0.0.1";
  const port = Number.parseInt(process.env.CONTRACT_MOCK_PORT ?? "4010", 10);
  const server = await createContractMockServer();
  server.listen(port, host, () => {
    console.log(`mmdash contract mock listening on http://${host}:${port}`);
  });
}
