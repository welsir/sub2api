## Why

The current moderation path depends on an OpenAI-shaped external service, while OpenAI billing activation is unavailable and hosted alternatives make roughly 50,000 daily checks unnecessarily expensive. A self-hosted Qwen3Guard runtime on the operator's Windows GPU can remove per-call provider cost, but it needs a stable compatibility contract, private connectivity, group-scoped rollout, and honest failure handling before it can protect production traffic.

## What Changes

- Add a repository-owned moderation adapter that accepts Bearer-authenticated `POST /v1/moderations` requests and translates Qwen3Guard-Gen classifications into the OpenAI-shaped result consumed by Sub2API.
- Package and document a Windows 11 + WSL2 deployment lane for Qwen3Guard-Gen 0.6B, with 4B retained as a benchmark candidate rather than assumed to be production-better.
- Connect the Sub2API host to the Windows moderation runtime over a private Tailscale path; no raw public Windows inference or adapter port is introduced.
- Extend Sub2API group scope with an explicit exemption list so every group is moderated by default and only the resolved `tml` group ID bypasses local moderation.
- Stage the change through contract tests, local fixtures, Windows reachability checks, `observe`, labeled-sample comparison, and an operator-approved switch to `pre_block`.
- Audit the complete outbound semantic context instead of only the last user message, including tool calls and tool outputs, without silently truncating text at 12,000 characters.
- Preserve non-blocking failure handling in `observe`, but make scoped `pre_block` requests fail closed after at most three total attempts so unavailable or malformed moderation can never continue to the downstream account.
- Compress large Sub2API-to-adapter requests with gzip while keeping a stable full-context prefix that can benefit from model-server prefix caching.
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
- Requires Tailscale on the Sub2API host and Windows machine, a dedicated adapter Bearer secret, disabled Windows sleep during service hours, and restart/recovery checks.
- Does not add an external gateway dependency or modify Sub2API billing and account scheduling.
- Does not claim production readiness until the powered-on Windows host, real Sub2API deployment version, representative labeled samples, and live failure drills have been verified.
