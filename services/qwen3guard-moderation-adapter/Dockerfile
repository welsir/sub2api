# syntax=docker/dockerfile:1.7
# [INPUT]: TypeScript source, locked build dependencies, and non-secret build metadata.
# [OUTPUT]: Minimal non-root image containing only compiled adapter runtime files.
# [POS]: Reproducible build and container boundary for verified versioned deployment.

ARG NODE_IMAGE=node:24-alpine

FROM ${NODE_IMAGE} AS build

WORKDIR /app

COPY package.json pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile

COPY tsconfig.json tsconfig.build.json ./
COPY src/qwen3guard-moderation-adapter ./src/qwen3guard-moderation-adapter
RUN pnpm run build

FROM ${NODE_IMAGE}

ARG VERSION=dev
ARG COMMIT=unknown

LABEL org.opencontainers.image.title="Sub2API Moderation Adapter" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"

ENV NODE_ENV=production \
    QWEN3GUARD_ADAPTER_HOST=0.0.0.0 \
    QWEN3GUARD_ADAPTER_PORT=8090

WORKDIR /app

COPY --chown=node:node package.json ./
COPY --from=build /app/dist ./dist

USER node

EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:' + (process.env.QWEN3GUARD_ADAPTER_PORT || '8090') + '/healthz').then((response) => { if (!response.ok) process.exit(1); }).catch(() => process.exit(1));"

CMD ["node", "dist/main.js"]
