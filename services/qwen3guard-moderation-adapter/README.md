# Qwen3Guard Moderation Adapter

Sub2API-owned standalone service that converts the local Qwen3Guard Chat
Completions contract into the OpenAI-shaped `POST /v1/moderations` contract
consumed by Sub2API content moderation.

The adapter remains a separate process from the Sub2API Go server and from the
loopback-only model backend. Deploy it on the Windows/WSL2 model host and expose
only the adapter port to the approved Sub2API tailnet identity.

## Local Commands

```bash
pnpm install
cp .env.example .env
pnpm test
pnpm typecheck
pnpm start
```

The service exposes:

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- Bearer-authenticated `POST /v1/moderations`

The backend base URL may include a fixed path prefix. The Moderations endpoint
accepts ordinary JSON and bounded `Content-Encoding: gzip` JSON; compressed and
decompressed bodies are both limited by `QWEN3GUARD_ADAPTER_MAX_BODY_BYTES`.
Configuration rejects
credentials, queries, fragments, backslashes, and dot-segment variants before
backend authorization is attached. Request bodies, backend streams, inference,
readiness, queueing, concurrency, and shutdown all use explicit bounds.

Sub2API sends a stable role-tagged full-context transcript. The adapter forwards
that transcript as one text message to Qwen3Guard. Model-server automatic prefix
caching can reuse the unchanged beginning for inference, while gzip reduces the
cloud-to-Windows request bytes. The adapter does not maintain session state or
reconstruct delta fragments in this first version.

Do not commit `.env`, adapter Bearer secrets, model-backend credentials, or
tailnet-specific addresses.
