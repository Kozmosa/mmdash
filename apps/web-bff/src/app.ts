import Fastify, { type FastifyInstance } from "fastify";

export type BuildAppOptions = {
  coreBaseUrl?: string;
  fetchImplementation?: typeof fetch;
};

export function buildApp(options: BuildAppOptions = {}): FastifyInstance {
  const app = Fastify({ logger: true });
  const coreBaseUrl =
    options.coreBaseUrl ?? process.env.CORE_BASE_URL ?? "http://localhost:8080";
  const fetchImplementation = options.fetchImplementation ?? fetch;

  app.get("/health/live", async () => ({ status: "ok" }));
  app.get("/api/example", async (request, reply) => {
    const requestId =
      typeof request.headers["x-request-id"] === "string"
        ? request.headers["x-request-id"]
        : crypto.randomUUID();
    const response = await fetchImplementation(`${coreBaseUrl}/v1/example`, {
      headers: { "x-request-id": requestId },
    });

    reply.header("x-request-id", requestId);
    reply.code(response.status);
    return reply.send(await response.json());
  });

  return app;
}
