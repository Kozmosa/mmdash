FROM node:22-alpine AS base
ENV COREPACK_NPM_REGISTRY=https://registry.npmmirror.com \
    pnpm_config_registry=https://registry.npmmirror.com
RUN corepack enable
WORKDIR /workspace

FROM base AS dependencies
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --frozen-lockfile --filter @mmdash/web...

FROM dependencies AS build
ARG BFF_INTERNAL_URL=http://web-bff:3001
ENV BFF_INTERNAL_URL=$BFF_INTERNAL_URL
COPY tsconfig.base.json ./
COPY apps/web apps/web
RUN pnpm --filter @mmdash/web build

FROM node:22-alpine AS runtime
ENV NODE_ENV=production
WORKDIR /app
COPY --from=build /workspace/apps/web/.next/standalone ./
COPY --from=build /workspace/apps/web/.next/static ./apps/web/.next/static
EXPOSE 3000
CMD ["node", "apps/web/server.js"]