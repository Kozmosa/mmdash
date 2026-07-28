FROM node:24-alpine AS base
RUN corepack enable
WORKDIR /workspace

FROM base AS dependencies
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/mcp-gateway/package.json apps/mcp-gateway/package.json
COPY packages/core-client/package.json packages/core-client/package.json
RUN pnpm install --frozen-lockfile --filter @mmdash/mcp-gateway...

FROM dependencies AS build
COPY tsconfig.base.json ./
COPY apps/mcp-gateway apps/mcp-gateway
COPY packages/core-client packages/core-client
RUN pnpm --filter @mmdash/core-client build
RUN pnpm --filter @mmdash/mcp-gateway build
RUN pnpm --filter @mmdash/mcp-gateway deploy --prod /out

FROM node:24-alpine AS runtime
ENV NODE_ENV=production
WORKDIR /app
COPY --from=build /workspace/apps/mcp-gateway/dist ./dist
COPY --from=build /workspace/packages/core-client/dist ./node_modules/@mmdash/core-client/dist
COPY --from=build /workspace/packages/core-client/package.json ./node_modules/@mmdash/core-client/package.json
COPY --from=build /out/node_modules ./node_modules
COPY --from=build /out/package.json ./
EXPOSE 3002
CMD ["node", "dist/main.js"]
