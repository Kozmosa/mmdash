ARG NODE_BASE_IMAGE=node:24-alpine

FROM ${NODE_BASE_IMAGE} AS base
ENV COREPACK_NPM_REGISTRY=https://registry.npmmirror.com \
    pnpm_config_registry=https://registry.npmmirror.com
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
RUN test -f /out/dist/main.js \
    && test -d "$(readlink -f /out/node_modules/@mmdash/core-client)/dist"

FROM ${NODE_BASE_IMAGE} AS runtime
ENV NODE_ENV=production
WORKDIR /app
COPY --from=build /out ./
EXPOSE 3002
CMD ["node", "dist/main.js"]
