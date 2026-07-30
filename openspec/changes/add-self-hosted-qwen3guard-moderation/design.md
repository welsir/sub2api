## Context

Sub2API owns content-moderation policy. The inspected development branch already accepts an OpenAI-shaped `POST <base>/v1/moderations` provider, scopes checks by the authenticated API key's `GroupID`, and supports `off`, `observe`, and `pre_block`. Its current semantic-provider error path is fail-open.

A read-only check of 43 V2 on 2026-07-30 verified the deployed image as `tml/sub2api:v0.1.156-omni-luna-first-text-20260728`, the independent database as `sub2api_v2`, and the active exact-name `tml` group as ID `18`. The live legacy moderation JSON is currently `enabled=true`, `mode=pre_block`, `all_groups=false`, `group_ids=[8,10,13,14,15]`, and `keyword_blocking_mode=keyword_only`; it has no `excluded_group_ids`, semantic-provider endpoint, or semantic-provider credential configured. These are point-in-time production facts and no live setting was changed.

Qwen3Guard-Gen produces structured `Safe`, `Controversial`, or `Unsafe` labels plus Qwen-specific categories. Its vLLM/SGLang deployment surface is Chat Completions compatible, not a direct Moderations API. A compatibility adapter is therefore required. The intended Windows host is currently powered off, so GPU runtime, WSL2, Tailscale reachability, latency, and model quality are explicitly unverified.

The expected volume is about 50,000 inbound requests per day, or roughly 0.58 requests per second on average. Peak concurrency and input-size distribution are more important than this average and must be measured before blocking production traffic. The available Windows hardware should comfortably load 0.6B and can plausibly test 4B, but model selection remains an evidence decision.

## Goals / Non-Goals

**Goals:**

- Provide the exact authenticated Moderations API contract Sub2API consumes.
- Translate Qwen3Guard output deterministically without presenting synthetic policy scores as calibrated probabilities.
- Keep the Windows inference endpoint private to the Sub2API host.
- Make moderation default-on for every Sub2API group while exempting only the resolved `tml` group ID.
- Make model unavailability, parse failure, overload, and network failure visible and bounded.
- Require measured model quality, latency, failure drills, and human-controlled promotion before `pre_block`.
- Preserve a fast rollback to the prior Sub2API moderation configuration.

**Non-Goals:**

- Introducing an external gateway dependency for content moderation.
- Changing Sub2API billing or account scheduling.
- Claiming that 0.6B or 4B is production-quality before a representative local benchmark.
- Auditing model output, images, audio, or arbitrary multimodal content in the first slice.
- Introducing a public inference endpoint, automatic rollout promotion, or an implicit fail-close policy.
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

`POST /v1/moderations` will require a dedicated Bearer secret and accept the request shape used by Sub2API: a model identifier and text input. Text arrays may be normalized deterministically, but image or other unsupported parts will return a clear non-2xx error rather than being silently labeled safe.

The successful response will contain at least one `results` item with `flagged`, `categories`, and `category_scores`. Contract fixtures will prove that Sub2API can deserialize and evaluate the response. The Base URL configured in Sub2API will be the adapter origin, because Sub2API appends `/v1/moderations`.

### 3. Treat generated scores as policy values, not probabilities

Qwen3Guard-Gen does not provide calibrated OpenAI category probabilities. The adapter will therefore use deterministic policy values:

- `Safe` maps to `0.0`.
- `Controversial` maps to `0.5`.
- `Unsafe` maps to `1.0`.

Qwen categories will map into Sub2API's evaluated category names. Violent content maps to `violence` and `illicit/violent`; non-violent illegal acts, jailbreaks, unethical acts, PII, copyright violations, and unsupported unsafe categories map to `illicit`; sexual content maps to `sexual`; suicide and self-harm maps to `self-harm`. Any `Unsafe` result with an absent or unknown category will set the `illicit` fallback to `1.0` so it cannot become an unflagged success solely because the taxonomies differ.

`flagged` will be true only for `Unsafe`. `Controversial` remains observable and can be tuned later, but the initial adapter will not pretend that `0.5` is a model confidence. Malformed, empty, or unknown Qwen output will return an adapter error and will never be converted to `Safe`.

### 4. Bound inference work and retain only operational metadata

The adapter will enforce an input limit aligned with or lower than Sub2API's normalized text limit, a bounded concurrency queue, an inference timeout, and a clear overload response. It will not implement an unbounded retry loop; Sub2API remains the caller that owns any request retry.

Adapter logs will contain request correlation, model version, label/category, input length or hash, latency, and error class. They will not contain full user prompts, Bearer secrets, or model-server credentials. The exact model revision and adapter mapping revision will be recorded with each deployment so later audits can distinguish behavior changes.

### 5. Use Tailscale as a private server-to-server path

The Windows/WSL2 endpoint will not be publicly exposed. Tailscale ACLs will permit only the Sub2API host identity to reach the adapter port. The adapter will still require Bearer authentication because tailnet membership alone is not application authorization.

The acceptance probe must originate from the running Sub2API container or its exact network namespace, not merely from the cloud host. The Windows host must have sleep disabled during service hours, deterministic service startup, and documented recovery after reboot, broadband reconnect, WSL restart, and model-process failure.

Alternatives considered:

- Raw router port forwarding and unauthenticated FRP are rejected because they create an unnecessary public attack surface.
- A public Cloudflare Tunnel is not required for a server-to-server path; it may be reconsidered only if the private route cannot satisfy availability and identity controls.

### 6. Add default-on scope with one explicit `tml` exemption

Sub2API remains the policy enforcement owner. The current `all_groups` plus `group_ids` model cannot safely express the approved rule. `all_groups=true` audits `tml`, while `all_groups=false` requires an allowlist that would silently omit future groups. The Sub2API configuration will therefore add normalized `excluded_group_ids`.

Scope evaluation will follow these rules:

- With `all_groups=true`, a request is audited unless its authenticated API key's non-null `GroupID` is in `excluded_group_ids`.
- With `all_groups=true`, a missing group ID is audited because it is not the explicitly exempt `tml` identity.
- With `all_groups=false`, the existing `group_ids` allowlist behavior remains unchanged for backward compatibility and exclusions are not mixed into the same mode.
- The exemption is stored by stable group ID, not by display-name matching. A renamed group retains its explicit exemption; a deleted and recreated group receives a new ID and is audited until an operator explicitly exempts it.
- The admin API, status output, logs, and UI expose the exemption list and distinguish `skip_group_exempt` from other scope skips.

The initial configuration will use:

- `all_groups=true`.
- Exactly the verified 43 V2 `tml` group ID `18` in `excluded_group_ids`.
- No other exempt group.
- `observe` during evidence collection.
- `keyword_and_api` so known hard keywords remain deterministic when the semantic service is unavailable.
- `pre_block` only after operator approval.

The current semantic-provider error behavior remains fail-open in the first slice. Errors, timeouts, and skipped checks must be visible in Sub2API and adapter evidence. A future fail-close option would change request-availability semantics and requires a separate Sub2API change and explicit approval.

### 7. Separate local proof, deployed proof, and production activation

Passing adapter unit and contract tests proves only local behavior. Reaching `/readyz` over Tailscale proves connectivity and model readiness, not moderation quality. `observe` evidence proves deployed classification behavior, not blocking. Only a manually approved `pre_block` switch plus blocked/allowed probes proves active enforcement for A.

No step automatically promotes the next one. The operator owns start, pause, group selection, and promotion.

## Risks / Trade-offs

- [Home power, sleep, broadband, or WSL failure makes the semantic service unavailable] -> Preserve fail-open initially, retain keyword blocking, alert on readiness/errors, configure restart behavior, and run outage drills before promotion.
- [Fail-open means A is not guaranteed to be semantically blocked during an outage] -> State this explicitly in operator UI/run evidence; require a separate approved fail-close design if guaranteed denial is required.
- [0.6B may miss nuanced Chinese or adversarial content] -> Benchmark 0.6B and 4B against the same labeled real samples and select on recall, false-positive rate, latency, and peak concurrency.
- [Synthetic scores may be mistaken for calibrated confidence] -> Name them policy scores in code and docs, keep fixed mapping tests, and retain raw Qwen label/category in adapter operational logs.
- [Taxonomy mismatch can hide unsafe categories] -> Maintain an explicit mapping table and an `Unsafe` fallback into a Sub2API-evaluated category.
- [Tailscale works from the host but not from the Sub2API container] -> Make the container-origin request a required connectivity gate.
- [Retries can multiply user latency while the home host is offline] -> Use bounded caller retries and set timeouts from measured P99 inference plus network headroom.
- [Windows GPU stack for RTX 5070 Ti may require newer driver/CUDA/runtime builds] -> Record exact versions, prove cold and warm starts, and keep backend selection behind the compatibility gate.
- [`tml` bypass can be misread as no safety policy anywhere] -> Document that `tml` skips only this local Sub2API moderation scope; upstream provider policy may still apply.
- [A hand-maintained non-`tml` allowlist misses newly created groups] -> Use default-on `all_groups=true` plus one explicit exclusion and test a newly created group before rollout.
- [Group display names are mutable or duplicated] -> Persist the verified 43 V2 `tml` group ID `18`; never grant exemption through case-insensitive name matching at request time.
- [Inbound-only moderation may be mistaken for output moderation] -> Label every test and status surface with the audited direction and reject output-coverage claims.

## Migration Plan

1. Implement adapter contract tests, deterministic mapping fixtures, auth, health/readiness, bounded execution, and redacted logging without a live Windows dependency.
2. On the powered-on Windows machine, record GPU/WSL2/runtime versions and prove Qwen3Guard-Gen 0.6B cold start, warm inference, and restart. Test 4B under the same bounded input and concurrency profile.
3. Establish the Tailscale ACL and prove authenticated `/readyz` and safe/unsafe Moderations fixtures from the actual Sub2API runtime boundary.
4. Implement and verify the Sub2API `excluded_group_ids` extension, retain the verified `tml` group ID `18`, then capture the complete rollback-safe deployed configuration and prepare disposable scope-test keys.
5. Configure `all_groups=true` with only `tml` excluded, begin in `observe` with `keyword_and_api`, and collect audited non-`tml` plus exempt-`tml` evidence without claiming blocking.
6. Compare 0.6B and 4B on a representative labeled sample and measured peak load. Record the selected model revision, mapping revision, timeout, retry count, and operator decision.
7. Run service-stop, Windows-reboot, tailnet-loss, overload, malformed-output, and recovery drills. Confirm fail-open and keyword fallback match the documented policy.
8. After explicit operator approval, switch non-`tml` traffic to `pre_block`, run allowed and blocked probes, and verify only `tml` bypasses local moderation.
9. Roll back by restoring the captured Sub2API configuration or returning to `observe`/`keyword_only`; do not require a Sub2API restart.

## Open Questions

- What are the measured peak RPS, P95/P99 input length, and acceptable moderation latency on the target traffic?
- Which disposable `tml`, existing non-`tml`, newly created, and ungrouped API keys will be used for scope proof?
- What labeled-sample recall and false-positive thresholds will the operator require before `pre_block`?
- Does vLLM or SGLang pass the RTX 5070 Ti WSL2 compatibility gate, or is the Transformers fallback required?
- Is 0.6B sufficient, or does 4B provide enough quality improvement to justify its latency and memory cost?
- Should a future Sub2API change add per-group fail-close, independent provider/model settings, or model-output moderation?
