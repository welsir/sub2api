## Why

The current moderation path depends on an OpenAI-shaped service and previously failed open or inspected only part of long conversations. The selected hosted candidate now uses `MiniMax-M3` with thinking disabled behind a repository-owned adapter, while a self-hosted Qwen3Guard runtime remains an optional later cost-reduction route. Either backend needs the same stable compatibility contract, complete-context coverage, group-scoped rollout, and honest failure handling before it can protect production traffic.

## What Changes

- Add a repository-owned moderation adapter that accepts Bearer-authenticated `POST /v1/moderations` requests and translates Qwen3Guard-Gen classifications into the OpenAI-shaped result consumed by Sub2API.
- Add a MiniMax backend using `MiniMax-M3` with thinking disabled and temperature zero, strict allow-only parsing, deterministic quota/auth failure handling, and mandatory real classification and latency probes before release.
- Retain the Windows 11 + WSL2 Qwen3Guard lane as optional future work; if used later, it remains private over Tailscale and does not expose a raw public inference port.
- Extend Sub2API group scope with an explicit exemption list so every group is moderated by default and only the resolved `tml` group ID bypasses local moderation.
- Stage the change through contract tests, local fixtures, Windows reachability checks, `observe`, labeled-sample comparison, and an operator-approved switch to `pre_block`.
- Audit the complete outbound semantic context instead of only the last user message, including tool calls and tool outputs, without silently truncating text at 12,000 characters.
- Preserve non-blocking failure handling in `observe`, but make scoped `pre_block` requests fail closed after at most three total attempts so unavailable or malformed moderation can never continue to the downstream account.
- Compress large Sub2API-to-adapter requests with gzip while keeping a stable full-context prefix that can benefit from model-server prefix caching.
- Preserve normal attachment-bearing requests by moderating only complete visible
  text and bounded non-secret attachment markers; do not OCR, fetch, or inspect
  image pixels or file bytes, and record pure attachment-content attacks as an
  explicit residual risk.
- Limit the first slice to request-side semantic moderation. Model-output and general multimodal moderation remain outside this change.

## Capabilities

### New Capabilities

- `qwen3guard-moderation-adapter`: OpenAI-shaped moderation contract, deterministic Qwen3Guard label/category translation, authentication, health reporting, and bounded inference behavior.
- `private-moderation-connectivity`: Private Sub2API-to-Windows reachability, endpoint protection, startup/recovery expectations, and connectivity validation without public exposure.
- `group-scoped-moderation-rollout`: Default-on moderation, explicit `tml` exemption semantics, observation and blocking gates, failure visibility, rollback, and acceptance evidence.

### Modified Capabilities

None.

## Impact

- Adds a small moderation adapter runtime plus Windows/WSL2 packaging, configuration examples, contract fixtures, smoke checks, and operator documentation to this repository.
- Uses Qwen3Guard-Gen through Transformers, vLLM, or SGLang behind the adapter; the model server's Chat Completions compatibility is not treated as direct `/v1/moderations` compatibility.
- Extends Sub2API with `excluded_group_ids` while preserving the existing `all_groups` and selected-group configuration behavior. A manually maintained allowlist of every non-`tml` group is not accepted because newly created groups would bypass moderation.
- Requires a dedicated adapter Bearer secret and a MiniMax credential with working inference capacity. `/v1/models` alone is not sufficient readiness proof. Tailscale, Windows sleep, and local-runtime recovery requirements apply only if the optional Qwen lane is activated later.
- Does not add an external gateway dependency or modify Sub2API billing and account scheduling.
- Does not claim production readiness until the powered-on Windows host, real Sub2API deployment version, representative labeled samples, and live failure drills have been verified.
