## Context

### 2026-08-02 MiniMax-first amendment

The operator approved a hosted MiniMax first rollout instead of requiring the
Windows Qwen runtime. This amendment supersedes the earlier external-provider
non-goal and default-on group rollout: the initial enforcement scope is only the
explicitly configured high-risk `group_ids`. False positives are acceptable, but
the rollout configuration MUST set `auto_ban_enabled=false`; MiniMax decisions do
not automatically ban users, disable API keys, or create a permanent session
block.

The adapter remains the stable `POST /v1/moderations` boundary and now selects a
`qwen` or `minimax` Chat Completions backend. MiniMax sensitive flags, provider
codes `1026`/`1027`, and uncertain classifier output are successful blocked
classifications. Auth and billing failures are deterministic; network, timeout,
throttling, and invalid HTTP-body failures remain visible for Sub2API's bounded
fail-closed policy. Windows, Tailscale, and local-model tasks remain optional
future work and are not production prerequisites for this rollout.

The first hosted candidate was `MiniMax-M2.7-highspeed`, but a credentialed local
comparison selected `MiniMax-M3` with thinking disabled and temperature zero:
the tested M2.7 high-speed path still emitted reasoning tokens and was slower.
The adapter retains M2.7 high-speed compatibility without attaching a separate
`service_tier`, but M3 is the current release candidate. A successful authenticated
`/v1/models` response proves only provider reachability and configured-model
presence; it does not prove that the credential has an active plan, remaining
quota, or working inference. Candidate readiness therefore requires a real
classification probe before any application-image switch.

Provider codes `2056` and `2062` are deterministic billing or quota
unavailability, not network jitter. The adapter surfaces them as a non-retryable
billing failure so Sub2API cannot multiply a plan/configuration problem across
the three-attempt transient retry budget. No one-second MiniMax SLA is assumed;
latency remains a measured release gate.

### 2026-08-03 text-only attachment amendment

The operator approved preserving normal attachment workflows rather than
blocking every request that contains structured media. Sub2API now constructs a
separate text-only moderation projection before account selection and leaves the
original request body unchanged. A strict text `allow` permits that original
request, including its attachments, to continue through the normal downstream
path; `block` or `review` rejects the entire request before any downstream
account is selected.

The projection covers ordered visible text and tool traffic across OpenAI Chat,
Responses, Images, Anthropic Messages, Gemini, and Grok media requests. Attachment
content becomes only a canonical marker containing bounded kind, normalized MIME,
source class, and normalized extension when available. Raw image pixels, file
bytes, Base64, URLs or URIs, opaque file IDs, file names, headers, cookies, and
credentials never enter the moderation transcript. A marker or unavailable
attachment content is neutral metadata, not an automatic allow, block, or review;
visible dangerous text still controls the decision.

This is an explicit residual-risk trade-off. Harmful instructions that exist only
inside image pixels or file bytes can pass text-only moderation and reach the
upstream model. V1 does not OCR, fetch, extract, scan archives or executables, or
claim multimodal coverage. The adapter still fail-closes a direct structured-media
Moderations call as defense in depth, but that is a protocol-violation path and is
not the normal Sub2API attachment flow.

Sub2API owns content-moderation policy. The inspected development branch already accepts an OpenAI-shaped `POST <base>/v1/moderations` provider, scopes checks by the authenticated API key's `GroupID`, and supports `off`, `observe`, and `pre_block`. The prior implementation audited only the last user message, silently truncated normalized text at 12,000 characters, and allowed scoped requests after semantic-provider failure.

A read-only check of 43 V2 on 2026-07-30 verified the deployed image as `tml/sub2api:v0.1.156-omni-luna-first-text-20260728`, the independent database as `sub2api_v2`, and the active exact-name `tml` group as ID `18`. The live legacy moderation JSON is currently `enabled=true`, `mode=pre_block`, `all_groups=false`, `group_ids=[8,10,13,14,15]`, and `keyword_blocking_mode=keyword_only`; it has no `excluded_group_ids`, semantic-provider endpoint, or semantic-provider credential configured. These are point-in-time production facts and no live setting was changed.

Qwen3Guard-Gen produces structured `Safe`, `Controversial`, or `Unsafe` labels plus Qwen-specific categories. Its vLLM/SGLang deployment surface is Chat Completions compatible, not a direct Moderations API. A compatibility adapter is therefore required. The intended Windows host is currently powered off, so GPU runtime, WSL2, Tailscale reachability, latency, and model quality are explicitly unverified.

The expected volume is about 50,000 inbound requests per day, or roughly 0.58 requests per second on average. Peak concurrency and input-size distribution are more important than this average and must be measured before blocking production traffic. The available Windows hardware should comfortably load 0.6B and can plausibly test 4B, but model selection remains an evidence decision.

## Goals / Non-Goals

**Goals:**

- Provide the exact authenticated Moderations API contract Sub2API consumes.
- Translate Qwen3Guard output deterministically without presenting synthetic policy scores as calibrated probabilities.
- Keep the Windows inference endpoint private to the Sub2API host.
- Apply strict moderation only to explicitly approved high-risk groups in the MiniMax-first rollout.
- Make model unavailability, parse failure, overload, and network failure visible, bounded, and fail closed for scoped `pre_block` requests.
- Audit the complete ordered outbound semantic context, including tool traffic, without silent text truncation.
- Preserve original attachment-bearing requests while exposing only bounded,
  non-secret attachment markers to the text classifier.
- Reduce repeated-context transfer with deterministic overlapping chunks, a Redis verdict cache, and gzip for uncached chunks.
- Require measured model quality, latency, failure drills, and human-controlled promotion before `pre_block`.
- Preserve a fast rollback to the prior Sub2API moderation configuration.

**Non-Goals:**

- Automatically banning users or disabling API keys from a MiniMax decision.
- Changing Sub2API billing or account scheduling.
- Claiming that 0.6B or 4B is production-quality before a representative local benchmark.
- Auditing model output, image pixels, audio, file bytes, archives, executables,
  or arbitrary multimodal content in the first slice.
- Fetching attachment URLs, resolving file IDs, OCR, document extraction, or
  malware analysis.
- Introducing a public inference endpoint, automatic rollout promotion, or provider-side session state in the first slice.
- Remotely configuring or validating the powered-off Windows machine during the planning phase.

## Decisions

### 1. Keep the adapter separate from the Sub2API Go process and from the model server

The Sub2API repository will own a small standalone moderation adapter under
`services/qwen3guard-moderation-adapter/` together with its deployment
artifacts. The Sub2API Go process will call the adapter; the adapter will call a
loopback-only Qwen3Guard model backend. This keeps the Moderations contract and
category policy testable without coupling GPU inference dependencies to the Go
API process.

The initial preferred backend is a Qwen-supported vLLM or SGLang Chat Completions server under WSL2. A Transformers backend is an allowed fallback only if the Windows GPU compatibility gate fails and the adapter backend contract remains unchanged.

Alternatives considered:

- Direct Sub2API-to-vLLM configuration is rejected because `/v1/chat/completions` does not satisfy `/v1/moderations`.
- Embedding Transformers in the Sub2API Go process is rejected because it mixes an optional Windows GPU runtime into the public API process and its failure domain.
- Exposing a third-party hosted moderation provider remains possible but does not meet the cost objective.

### 2. Implement a narrow OpenAI-shaped contract

The adapter will expose:

- `GET /healthz` for process liveness.
- `GET /readyz` for model-backend readiness.
- `POST /v1/moderations` for authenticated text moderation.

`POST /v1/moderations` will require a dedicated Bearer secret and accept the
normal request shape used by Sub2API: a model identifier and one text input. The
text may contain canonical attachment markers, but Sub2API does not send image
parts or file bodies to this endpoint. If a caller bypasses that projection and
sends a structured image directly, the adapter returns a deterministic flagged
local-policy result without invoking MiniMax. This defense-in-depth behavior is
not image-pixel moderation and is not the normal Sub2API request path.

The successful response will contain at least one `results` item with `flagged`, `categories`, and `category_scores`. Contract fixtures will prove that Sub2API can deserialize and evaluate the response. The Base URL configured in Sub2API will be the adapter origin, because Sub2API appends `/v1/moderations`.

### 3. Treat generated scores as policy values, not probabilities

Qwen3Guard-Gen does not provide calibrated OpenAI category probabilities. The adapter will therefore use deterministic policy values:

- `Safe` maps to `0.0`.
- `Controversial` maps to `0.5`.
- `Unsafe` maps to `1.0`.

Qwen categories will map into Sub2API's evaluated category names. Violent content maps to `violence` and `illicit/violent`; non-violent illegal acts, jailbreaks, unethical acts, PII, copyright violations, and unsupported unsafe categories map to `illicit`; sexual content maps to `sexual`; suicide and self-harm maps to `self-harm`. Any `Unsafe` result with an absent or unknown category will set the `illicit` fallback to `1.0` so it cannot become an unflagged success solely because the taxonomies differ.

`flagged` will be true only for `Unsafe`. `Controversial` remains observable and can be tuned later, but the initial adapter will not pretend that `0.5` is a model confidence. Malformed, empty, or unknown Qwen output will return an adapter error and will never be converted to `Safe`.

### 4. Bound inference work and retain only operational metadata

The adapter will enforce explicit compressed-body, decompressed-body, model-input, concurrency, queue, and inference limits. It accepts gzip request bodies and rejects malformed or over-limit payloads instead of silently labeling or truncating them. Sub2API remains the caller that owns bounded retries.

Adapter logs will contain request correlation, model version, label/category, input length or hash, latency, and error class. They will not contain full user prompts, Bearer secrets, or model-server credentials. The exact model revision and adapter mapping revision will be recorded with each deployment so later audits can distinguish behavior changes.

### 5. Use Tailscale as a private server-to-server path

The Windows/WSL2 endpoint will not be publicly exposed. Tailscale ACLs will permit only the Sub2API host identity to reach the adapter port. The adapter will still require Bearer authentication because tailnet membership alone is not application authorization.

The acceptance probe must originate from the running Sub2API container or its exact network namespace, not merely from the cloud host. The Windows host must have sleep disabled during service hours, deterministic service startup, and documented recovery after reboot, broadband reconnect, WSL restart, and model-process failure.

Alternatives considered:

- Raw router port forwarding and unauthenticated FRP are rejected because they create an unnecessary public attack surface.
- A public Cloudflare Tunnel is not required for a server-to-server path; it may be reconsidered only if the private route cannot satisfy availability and identity controls.

### 6. Retain both scope modes and start with the approved high-risk allowlist

Sub2API remains the policy enforcement owner. It supports normalized
`excluded_group_ids` for a future default-on rollout, but the MiniMax-first
deployment intentionally uses the existing selected-group allowlist so only the
approved high-risk groups incur hosted-classifier cost and latency.

Scope evaluation will follow these rules:

- With `all_groups=true`, a request is audited unless its authenticated API key's non-null `GroupID` is in `excluded_group_ids`.
- With `all_groups=true`, a missing group ID is audited because it is not the explicitly exempt `tml` identity.
- With `all_groups=false`, the existing `group_ids` allowlist behavior remains unchanged for backward compatibility and exclusions are not mixed into the same mode.
- The exemption is stored by stable group ID, not by display-name matching. A renamed group retains its explicit exemption; a deleted and recreated group receives a new ID and is audited until an operator explicitly exempts it.
- The admin API, status output, logs, and UI expose the exemption list and distinguish `skip_group_exempt` from other scope skips.

The initial configuration will use:

- `all_groups=false`.
- The approved high-risk group IDs `8`, `10`, `13`, `14`, and `15` in
  `group_ids`.
- No `excluded_group_ids` in this allowlist mode.
- `keyword_and_api` so known hard keywords remain deterministic when the semantic service is unavailable.
- `pre_block` after isolated candidate verification.
- `auto_ban_enabled=false` so one classifier result cannot disable a user or Key.

`observe` remains fail-open because it is evidence collection rather than enforcement. Sampling applies only to `observe`; `pre_block` audits every in-scope request. In `pre_block`, the existing group and model scope is also the failure-policy scope: no audit Key, timeout, network error, overload, non-2xx, malformed response envelope, empty result, or Qwen parse failure returns a local 503 after bounded attempts and cannot continue to downstream account selection. MiniMax classifier-shape uncertainty instead returns a successful blocked result without retries. The default `retry_count=2` means three total attempts. Deterministic 400/401/403 errors may terminate immediately; transient failures may reuse the only configured audit Key within the current request even if its health state is frozen for later requests.

### 7. Audit complete context with deterministic incremental chunks

Sub2API will construct an ordered role-tagged transcript from all visible
semantic text that is about to be sent upstream: system/developer instructions,
user and assistant messages, function/tool calls and outputs, Responses
instructions, Gemini function traffic, OpenAI Images or Grok media prompts, and
top-level tool/output schemas that can carry names, descriptions, instructions,
examples, defaults, or schema text. This includes Chat `tools`, legacy
`functions`, and `response_format`; Responses `tools` and `text.format`;
Anthropic tool/output schemas; and Gemini tool declarations and response schemas. Unknown content
blocks receive a generic semantic fallback through the same safe projection. A
final `Continue`, assistant item, or tool result never causes historical context
to be skipped.

OpenAI Chat `image_url`/file content, Responses `input_image`/`input_file`,
Anthropic image/document blocks, Gemini inline/file data, and OpenAI Images or
Grok reference/upload media become canonical text markers. Markers retain only
bounded `kind`, normalized `mime`, the source class
`inline|remote|file_id|upload`, and a normalized extension; `source=file_id`
describes the reference class and never contains the opaque ID. Raw URLs, URIs,
queries, file IDs, file names, Base64, binary bytes, headers, cookies, and
credentials are discarded from the projection. The original client request is
not rewritten by this extraction and remains available to the downstream path
after a strict allow.

Reference schemes and MIME values are client-controlled. Unknown URL/URI
schemes never pass through as raw text, and only a bounded MIME allowlist may
appear in a marker. Low-entropy Base64 is omitted under attachment or binary
semantic keys without globally deleting identical ordinary user text. Non-empty
invalid JSON and bounded-projection failures are explicit failures. For
Responses WebSocket turns after the first, a current-frame projection failure
closes locally and cannot fall back to auditing only accumulated history.

The 12,000-rune silent truncation is removed. The normalized transcript is split
into deterministic 32,768-rune windows with a 1,024-rune overlap. Every rune is
covered by at least one chunk, and appending a turn keeps completed prefix chunk
hashes stable. Sub2API batch-reads versioned verdicts from Redis, blocks
immediately on a cached unsafe chunk, and sends only cache misses to the adapter
with bounded parallelism under one overall moderation deadline.

Safe verdicts expire after 24 hours and blocked verdicts after 30 days. Cache
keys contain only a policy namespace and SHA-256 chunk hash; prompt text is not
stored in Redis. The text projection revision
`incremental-full-context-v2-attachment-text-only` and expected classifier
policy revision `minimax-strict-policy-v5` both participate in the chunk hash
and Redis namespace, so verdicts from the prior projection or classifier policy
cannot be reused. The adapter exposes its classifier policy revision from
`/readyz` and every successful Moderations response. Sub2API requires a matching
`/readyz` revision before the first cache read for each configured
base-URL/revision pair, then requires the same revision on each uncached
Moderations result before writing its verdict. A response mismatch invalidates
the remembered readiness check so the next request verifies `/readyz` again.
Missing or mismatched revision evidence, Redis read/write errors, unavailable cache
support, provider errors, and deadline exhaustion return a local 503 in scoped
`pre_block` and do not select a downstream account.

For each uncached Moderations JSON larger than 1 KiB, Sub2API uses gzip
best-speed compression. This is a content-addressed verdict cache, not a
provider-side session protocol: full semantic coverage is reconstructed locally
on every request, while only unknown chunks cross the network.

### 8. Separate local proof, deployed proof, and production activation

Passing adapter unit and contract tests proves only local behavior. Reaching `/readyz` over Tailscale proves connectivity and model readiness, not moderation quality. `observe` evidence proves deployed classification behavior, not blocking. Only a manually approved `pre_block` switch plus blocked/allowed probes proves active enforcement for A.

No step automatically promotes the next one. The operator owns start, pause, group selection, and promotion.

## Risks / Trade-offs

- [Home power, sleep, broadband, or WSL failure makes the semantic service unavailable] -> In `pre_block`, return a local 503 after bounded attempts; configure restart behavior and run outage drills before promotion.
- [The hosted classifier may miss nuanced Chinese or adversarial content] -> Replay the selected MiniMax model against the same privacy-reviewed real and synthetic corpus, require every confirmed upstream policy case to block, and record false positives separately.
- [Synthetic scores may be mistaken for calibrated confidence] -> Name them policy scores in code and docs, keep fixed mapping tests, and retain raw Qwen label/category in adapter operational logs.
- [Taxonomy mismatch can hide unsafe categories] -> Maintain an explicit mapping table and an `Unsafe` fallback into a Sub2API-evaluated category.
- [Tailscale works from the host but not from the Sub2API container] -> Make the container-origin request a required connectivity gate.
- [Retries can multiply user latency while the home host is offline] -> Cap the path at three total attempts and set the per-attempt timeout from measured P99 inference plus network headroom.
- [Complete histories increase bytes and inference work] -> Reuse versioned Redis verdicts for stable chunks, gzip misses, and bound cold-chunk parallelism plus the overall deadline.
- [An attachment contains harmful instructions that are not present in visible
  request text] -> Preserve the user workflow but record this as an accepted V1
  false-negative risk; do not claim OCR, file inspection, malware analysis, or
  multimodal moderation.
- [An old cached allow survives a projection or classifier-policy change] -> Pin
  the expected classifier revision, verify it at readiness and response time,
  and include both projection and classifier revisions in chunk hashes and Redis
  namespaces.
- [Windows GPU stack for RTX 5070 Ti may require newer driver/CUDA/runtime builds] -> Record exact versions, prove cold and warm starts, and keep backend selection behind the compatibility gate.
- [A high-risk allowlist does not automatically include future groups] -> Treat the five approved IDs as an explicit cost/safety scope and require operator review before changing it.
- [Display names can be mutable or duplicated] -> Persist and evaluate stable group IDs; never infer moderation scope from a name at request time.
- [Group display names are mutable or duplicated] -> Persist the verified 43 V2 `tml` group ID `18`; never grant exemption through case-insensitive name matching at request time.
- [Inbound-only moderation may be mistaken for output moderation] -> Label every test and status surface with the audited direction and reject output-coverage claims.

## Migration Plan

1. Implement adapter contract tests, deterministic mapping fixtures, auth, health/readiness, bounded execution, and redacted logging without a live Windows dependency.
2. Keep the Windows Qwen lane deferred. For the hosted candidate, verify a credentialed `MiniMax-M3` classification with thinking disabled from the production-equivalent runtime boundary; `/v1/models` alone is insufficient.
3. Prove authenticated adapter `/readyz` and safe/unsafe Moderations fixtures from the actual Sub2API runtime boundary without exposing credentials in evidence.
4. Implement and verify complete text-only transcript extraction, attachment
   marker privacy, classifier-policy revision handshake, and the versioned Redis
   chunk-verdict cache, then capture the rollback-safe deployed configuration.
5. Keep `all_groups=false` with approved high-risk group IDs `8`, `10`, `13`, `14`, and `15`, enable `keyword_and_api`, and verify the incremental switch in an isolated candidate before changing the active application image.
6. Replay approximately 1,200 to 1,400 privacy-reviewed samples through `MiniMax-M3`, including every confirmed upstream policy case, risky candidates, legitimate reverse engineering, normal business, long contexts, `Continue`, and tool loops. Record model revision, mapping revision, latency, timeout, retry count, and operator decision without retaining prompt text in test output.
7. Run adapter-stop, provider-timeout/throttling/billing, Redis-loss, overload, malformed-output, and recovery drills. Confirm `observe` remains non-blocking and scoped `pre_block` returns a local 503 without downstream account selection.
8. After explicit operator approval, switch the isolated candidate into service, run allowed and blocked probes, and verify out-of-scope groups remain unaffected.
9. Roll back by restoring the captured Sub2API configuration or returning to `observe`/`keyword_only`; do not require a Sub2API restart.

## Open Questions

- What are the measured peak RPS, P95/P99 input length, and acceptable moderation latency on the target traffic?
- Which disposable `tml`, existing non-`tml`, newly created, and ungrouped API keys will be used for scope proof?
- What labeled-sample recall and false-positive thresholds will the operator require before `pre_block`?
- Does vLLM or SGLang pass the RTX 5070 Ti WSL2 compatibility gate, or is the Transformers fallback required?
- Is 0.6B sufficient, or does 4B provide enough quality improvement to justify its latency and memory cost?
- What are the measured cold-cache and appended-turn P50/P95/P99 latencies after the Redis chunk cache is enabled on 43 V2?
