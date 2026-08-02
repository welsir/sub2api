# Moderation Adapter

Sub2API-owned standalone service that converts Qwen3Guard or MiniMax Chat
Completions into the OpenAI-shaped `POST /v1/moderations` contract consumed by
Sub2API content moderation.

The adapter remains a separate process from the Sub2API Go server. It can call a
loopback-only Qwen backend or the MiniMax API selected by
`QWEN3GUARD_BACKEND_PROVIDER`.

## Local Commands

```bash
pnpm install
cp .env.example .env
pnpm test
pnpm typecheck
pnpm start
```

## Container Commands

Build the adapter without including any runtime credentials in image layers:

```bash
docker build \
  --build-arg VERSION=local \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --tag sub2api-moderation-adapter:local .
```

Supply MiniMax and adapter Bearer tokens only when the container starts. Do not
use Docker build arguments for secrets. The image listens on port `8090`, runs
as the non-root `node` user, and uses `/healthz` for its Docker liveness check.
Deployment tooling must probe `/readyz` separately before routing Sub2API to the
adapter because readiness verifies the configured backend connection.

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
that transcript to the selected backend. Qwen receives one user message. MiniMax
receives a fixed trusted classifier instruction plus the transcript as a separate
untrusted user message and accepts only a strict final JSON decision. MiniMax
`input_sensitive`, `output_sensitive`, `1026`, and `1027` results are returned as
normal blocked Moderations decisions. Authentication and billing failures remain
deterministic 4xx responses; transient and parse failures remain retryable 5xx
responses for Sub2API's bounded fail-closed policy.

For the low-latency MiniMax path, use `https://api.minimaxi.com`, `MiniMax-M3`,
`/v1/models`, `QWEN3GUARD_MINIMAX_SERVICE_TIER=priority`, and a dedicated
external Bearer token. The adapter disables M3 thinking for this deterministic
classifier request. `standard` remains available for lower-cost testing, and
M2.x remains compatible but cannot disable thinking. Never place the token in
Sub2API source or this repository. Enable `pre_block` only for the approved
high-risk `group_ids` and set Sub2API `auto_ban_enabled=false` for this rollout.
The adapter itself never bans users or disables API keys.

Stable transcript prefixes can benefit from provider prompt caching, while gzip
reduces request bytes. The adapter does not maintain session state or reconstruct
delta fragments in this first version.

Do not commit `.env`, adapter Bearer secrets, Qwen credentials, MiniMax tokens,
or tailnet-specific addresses.
