import websocket from "@fastify/websocket";
import { CoreClient } from "@mmdash/core-client";
import Fastify, { type FastifyInstance } from "fastify";
import { randomUUID } from "node:crypto";

import {
  createDefaultPageRegistry,
  type PageAggregatorRegistry,
} from "./aggregation/page-aggregator.js";
import { registerBrowserAuth } from "./auth/browser-auth.js";
import { type BffConfig, loadConfig } from "./config.js";
import { registerProjectContext } from "./context/project-context.js";
import { registerErrorHandler } from "./errors/error-handler.js";
import { registerHttpStreamRoutes } from "./proxy/http-streams.js";
import { registerPublicCoreApiProxy } from "./proxy/public-core-api.js";
import { registerWebSocketRoutes } from "./proxy/websocket.js";
import { registerExampleRoutes } from "./routes/example.js";
import { registerArtifactRoutes } from "./routes/artifacts.js";
import { registerAuthRoutes } from "./routes/auth.js";
import { registerContextProposalRoutes } from "./routes/context-proposals.js";
import { registerHealthRoutes } from "./routes/health.js";
import { registerPageRoutes } from "./routes/pages.js";
import { registerProjectRoutes } from "./routes/projects.js";
import { registerProgressRoutes } from "./routes/progress.js";
import { registerModelRoutes } from "./routes/models.js";
import { registerRepoRoutes } from "./routes/repo.js";
import { registerRepoWebhookRoutes } from "./routes/repo-webhook.js";
import { registerSettingsRoutes } from "./routes/settings.js";
import { registerNotificationRoutes } from "./routes/notification.js";
import { registerAgentRoutes } from "./routes/agent.js";
import { registerExperimentRoutes } from "./routes/experiments.js";
import {
  ArticleCollaboration,
  registerArticleCollaboration,
} from "./article/collaboration.js";
import { registerArticleRoutes } from "./routes/article.js";
import { registerBoxRoutes } from "./routes/box.js";

export type BuildAppOptions = {
  config?: BffConfig;
  coreClient?: CoreClient;
  fetchImplementation?: typeof fetch;
  logger?: boolean;
  pageRegistry?: PageAggregatorRegistry;
};

const requestIdPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/;

export function buildApp(options: BuildAppOptions = {}): FastifyInstance {
  const config = options.config ?? loadConfig();
  const app = Fastify({
    genReqId(request) {
      const candidate = request.headers["x-request-id"];
      return typeof candidate === "string" && requestIdPattern.test(candidate)
        ? candidate
        : randomUUID();
    },
    logger: options.logger ?? true,
    routerOptions: { maxParamLength: 4_096 },
  });
  const coreClient =
    options.coreClient ??
    new CoreClient(config.coreBaseUrl, options.fetchImplementation);
  const pageRegistry = options.pageRegistry ?? createDefaultPageRegistry();

  app.register(websocket, {
    errorHandler(error, socket, request) {
      request.log.error({ err: error }, "websocket handler failed");
      socket.close(1011, "WebSocket proxy failed");
    },
    options: {
      maxPayload: 4 * 1024 * 1024,
    },
  });
  registerBrowserAuth(app, coreClient, config);
  registerProjectContext(app);
  registerErrorHandler(app);

  app.addHook("onSend", async (request, reply, payload) => {
    reply.header("x-request-id", request.id);
    return payload;
  });
  app.addContentTypeParser("*", (request, payload, done) => {
    done(null, payload);
  });

  registerHealthRoutes(app, coreClient, config.version);
  registerAuthRoutes(app, coreClient, config);
  registerExampleRoutes(app, coreClient);
  registerArtifactRoutes(app, coreClient);
  registerBoxRoutes(app, coreClient);
  registerAgentRoutes(app, coreClient);
  registerExperimentRoutes(app, coreClient);
  const articleCollaboration = new ArticleCollaboration(coreClient);
  app.register(async function articleCollaborationScope(scopedApp) {
    registerArticleCollaboration(scopedApp, coreClient, articleCollaboration);
  });
  registerArticleRoutes(app, coreClient, articleCollaboration);
  registerContextProposalRoutes(app, coreClient);
  registerProjectRoutes(app, coreClient);
  registerProgressRoutes(app, coreClient);
  registerModelRoutes(app, coreClient);
  registerRepoRoutes(app, coreClient);
  registerRepoWebhookRoutes(app, coreClient);
  registerSettingsRoutes(app, coreClient);
  registerNotificationRoutes(app, coreClient);
  registerPageRoutes(app, coreClient, pageRegistry);
  registerHttpStreamRoutes(app, coreClient);
  registerPublicCoreApiProxy(app, coreClient);
  app.register(async function websocketRoutesScope(scopedApp) {
    registerWebSocketRoutes(scopedApp, coreClient);
  });

  return app;
}
