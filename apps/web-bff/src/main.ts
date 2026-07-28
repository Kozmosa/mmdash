import { buildApp } from "./app.js";

const port = Number.parseInt(process.env.BFF_PORT ?? "3001", 10);
const host = process.env.BFF_HOST ?? "0.0.0.0";
const app = buildApp();

try {
  await app.listen({ host, port });
} catch (error) {
  app.log.error(error);
  process.exitCode = 1;
}
