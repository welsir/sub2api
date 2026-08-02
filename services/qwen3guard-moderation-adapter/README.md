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

With incremental review enabled, Sub2API derives a stable role-tagged
full-context transcript, reuses versioned Redis verdicts, and sends one uncached
overlapping chunk per Moderations call. The adapter forwards that chunk to the
selected backend. Qwen receives one user message. MiniMax receives a fixed
trusted classifier instruction plus the chunk as a separate untrusted user
message and accepts only a strict final JSON decision. MiniMax
`input_sensitive`, `output_sensitive`, `1026`, and `1027` results are returned as
normal blocked Moderations decisions. Authentication and billing failures remain
deterministic 4xx responses. Missing choices, non-stop finishes, empty content,
and malformed MiniMax classifier decisions return normal blocked Moderations
results instead of retrying a large chunk. Network, timeout, throttling, and
invalid HTTP-body failures remain retryable 5xx responses for Sub2API's bounded
fail-closed policy.

The configured Qwen and MiniMax text backends do not inspect image contents.
OpenAI-shaped structured inputs containing `image_url` therefore receive a
deterministic local blocked decision without invoking the text backend or OAI.
This conservative boundary must remain until a separately verified vision
moderation backend is available; it must not silently treat uninspected images
as safe.

For the low-latency MiniMax path, use `https://api.minimaxi.com`, `MiniMax-M3`,
`/v1/models`, `QWEN3GUARD_MINIMAX_SERVICE_TIER=priority`, and a dedicated
external Bearer token. The adapter disables M3 thinking for this deterministic
classifier request. `standard` remains available for lower-cost testing, and
M2.x remains compatible but cannot disable thinking. Never place the token in
Sub2API source or this repository. Enable `pre_block` only for the approved
high-risk `group_ids` and set Sub2API `auto_ban_enabled=false` for this rollout.
The adapter itself never bans users or disables API keys.

Sub2API's content-addressed Redis cache stores only versioned chunk hashes and
compact verdicts: safe entries live 24 hours and blocked entries 30 days. It
reconstructs full coverage locally on every request, sends only misses with
bounded concurrency under one overall deadline, and gzip-compresses larger
chunk requests. The adapter does not maintain provider-side session state or
reconstruct delta fragments.

Do not commit `.env`, adapter Bearer secrets, Qwen credentials, MiniMax tokens,
or tailnet-specific addresses.
