FROM node:24-alpine AS base
ENV COREPACK_NPM_REGISTRY=https://registry.npmmirror.com \
    pnpm_config_registry=https://registry.npmmirror.com
RUN corepack enable
WORKDIR /workspace

FROM base AS dependencies
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web-bff/package.json apps/web-bff/package.json
COPY packages/core-client/package.json packages/core-client/package.json
RUN pnpm install --frozen-lockfile --filter @mmdash/web-bff...

FROM dependencies AS build
COPY tsconfig.base.json ./
COPY apps/web-bff apps/web-bff
COPY packages/core-client packages/core-client
RUN pnpm --filter @mmdash/core-client build
RUN pnpm --filter @mmdash/web-bff build
RUN pnpm --filter @mmdash/web-bff deploy --prod /out
RUN test -f /out/dist/main.js \
    && test -d "$(readlink -f /out/node_modules/@mmdash/core-client)/dist"

FROM node:24-alpine AS runtime
ENV NODE_ENV=production
WORKDIR /app
COPY --from=build /out ./
EXPOSE 3001
CMD ["node", "dist/main.js"]
