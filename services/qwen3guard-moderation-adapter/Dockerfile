# syntax=docker/dockerfile:1.7
# [INPUT]: Checked-in JavaScript runtime files and non-secret build metadata.
# [OUTPUT]: Minimal non-root moderation-adapter image with an HTTP liveness probe.
# [POS]: Reproducible container boundary for local verification and versioned deployment.

ARG NODE_IMAGE=node:24-alpine

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
COPY --chown=node:node src/qwen3guard-moderation-adapter ./src/qwen3guard-moderation-adapter

USER node

EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:' + (process.env.QWEN3GUARD_ADAPTER_PORT || '8090') + '/healthz').then((response) => { if (!response.ok) process.exit(1); }).catch(() => process.exit(1));"

CMD ["node", "src/qwen3guard-moderation-adapter/main.js"]
