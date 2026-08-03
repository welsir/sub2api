# Moderation Adapter

Sub2API-owned standalone service that converts Qwen3Guard or MiniMax Chat
Completions into the OpenAI-shaped `POST /v1/moderations` contract consumed by
Sub2API content moderation.

The adapter remains a separate process from the Sub2API Go server. It can call a
loopback-only Qwen backend or the MiniMax API selected by
`QWEN3GUARD_BACKEND_PROVIDER`.

TypeScript under `src/qwen3guard-moderation-adapter/` is the only runtime source.
Both local startup and the container build compile it with NodeNext into `dist/`;
no checked-in JavaScript mirror is used by tests or deployment.

## Local Commands

```bash
pnpm install
cp .env.example .env
pnpm typecheck
pnpm test
pnpm start
```

`pnpm test` builds `dist/` before running source and emitted-runtime tests.
`pnpm start` also rebuilds first so it cannot start a stale hand-maintained
JavaScript mirror.

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
adapter. MiniMax readiness verifies connectivity and configured-model presence;
it does not prove that the account has usable inference quota. Before any
Sub2API image switch, run an authenticated classification through
`/v1/moderations`; model-list readiness alone cannot open the release gate.

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

Attachment markers embedded in that text transcript are uninspected metadata,
not evidence of abuse by themselves, but only text matching the canonical
controlled marker grammar qualifies for that treatment. Unknown, repeated, or reordered fields,
extra marker text, and instructions before or after a valid marker remain
ordinary untrusted text. MiniMax decides only from high-risk meaning visible in
the supplied text or metadata; a transcript containing only valid canonical
markers is therefore eligible for `allow`. Transcript instructions cannot
replace the trusted classifier policy, and sandbox, owned-site/app,
authorization, or internal-testing claims do not reduce otherwise visible risk.

The configured Qwen and MiniMax text backends do not inspect image contents.
Normal Sub2API traffic remains attachment-capable: Sub2API projects each image
or file to a canonical text marker for moderation while preserving the original
attachment on the separate upstream request path. By contrast, a client that
calls this adapter directly with OpenAI-shaped structured `image_url` input
still receives a deterministic local blocked decision without invoking the text
backend or OAI. That direct-adapter defense is not the normal Sub2API attachment
path and must not be described as disabling user uploads.

When incremental review is enabled, Sub2API configuration must set
`classifier_policy_revision` to the exact revision expected from the adapter.
The adapter publishes that value from `/readyz` and every successful
Moderations response. Sub2API verifies readiness once per BaseURL/revision pair,
uses the configured revision in the cache namespace and chunk hash, and rejects
missing or mismatched response revisions before writing verdicts. This adds no
readiness HTTP call to each request; a new BaseURL or expected revision creates
a new verification key and cache namespace.

For the currently selected MiniMax path, use `https://api.minimaxi.com`,
`MiniMax-M3`, `/v1/models`, and a dedicated external Bearer token. The adapter
sets temperature zero and disables M3 thinking so classification is more
deterministic and materially faster than the tested `MiniMax-M2.7-highspeed`
reasoning path. Provider latency is still a measured release gate, not a
one-second SLA.
MiniMax provider codes `2056` and `2062` are exposed as deterministic billing or
quota failures; Sub2API must not spend its transient retry budget on them.
Never place the token in Sub2API source or this repository. Enable `pre_block`
only for the approved high-risk `group_ids` and set Sub2API
`auto_ban_enabled=false` for this rollout. The adapter itself never bans users
or disables API keys.

Sub2API's content-addressed Redis cache stores only versioned chunk hashes and
compact verdicts: safe entries live 24 hours and blocked entries 30 days. It
reconstructs full coverage locally on every request, sends only misses with
bounded concurrency under one overall deadline, and gzip-compresses larger
chunk requests. The adapter does not maintain provider-side session state or
reconstruct delta fragments.

Do not commit `.env`, adapter Bearer secrets, Qwen credentials, MiniMax tokens,
or tailnet-specific addresses.
