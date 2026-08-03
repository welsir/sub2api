# MiniMax-First Local Candidate Evidence

Date: 2026-08-02; attachment-contract and real MiniMax evidence updated 2026-08-03

## Boundary

- Repository: `/Users/welsir/.config/superpowers/worktrees/sub2api/omni-latest-selected-customs`
- Product branch: `dev/omni`
- This checkpoint changed and tested only the isolated local worktree.
- No production configuration, container, database, traffic route, or other branch was changed.
- Archived request bodies and prompts remained local. Tests emit aggregate counts only and never print prompt text, user IDs, API keys, account IDs, credentials, or request bodies.

## Integrated Candidate

- The current hosted candidate is `MiniMax-M3` with thinking disabled and
  temperature zero. `MiniMax-M2.7-highspeed` remains supported and omits
  `service_tier`, but the real comparison found that it still emitted reasoning
  tokens and was slower for this short classification contract.
- Provider codes `2056` and `2062` are deterministic billing/quota failures. They do not consume the transient retry budget.
- A generic MiniMax HTTP 429 remains transient and may use the bounded retry path.
- TypeScript is the only adapter runtime source. Tests and the multi-stage Docker image execute the same compiled `dist/` output; hand-maintained JavaScript mirrors were removed.
- Readiness requires a successful model-list response containing the configured model. It does not claim usable inference quota, so one authenticated `/v1/moderations` classification remains a separate release gate.
- Sub2API reviews the ordered outbound context, including instructions, assistant/tool history, tool output, and a final `Continue` turn. Long context is chunked with overlap and no 12,000-rune truncation.
- OpenAI Chat/Responses/Images, Anthropic, Gemini, Grok, and recognized tool
  attachment payloads are projected to a separate text-only transcript. The
  original downstream request remains unchanged after a strict text allow.
- Canonical markers contain only bounded kind, normalized MIME, source class,
  and normalized extension. They never contain raw URLs/URIs, opaque file ID
  values, file names, Base64, binary content, headers, cookies, or credentials.
- Attachment presence and unavailable attachment contents are neutral metadata,
  not an automatic allow, block, review, or moderation-service error. Visible
  dangerous text still blocks the complete request.
- The expected classifier policy revision is
  `minimax-strict-policy-v5`. Adapter readiness and successful Moderations
  responses expose that revision. A readiness mismatch fails closed before the
  first cache read for its base-URL/revision pair; a response mismatch fails
  closed before verdict write and invalidates the remembered readiness check.
  The projection revision
  `incremental-full-context-v2-attachment-text-only` and classifier revision
  both participate in chunk hashes and Redis namespaces.
- Transient moderation failures use at most three total attempts. Deterministic request, authentication, and billing failures stop immediately. A final moderation/cache failure remains fail-closed before downstream account selection, usage recording, or upstream audit.

## Synthetic Verification

The 2026-08-02 pre-amendment baseline passed locally:

- Adapter `pnpm typecheck`, `pnpm test`, and `pnpm build`: 7 files and 127 tests passed.
- Adapter Docker image built without cache. Runtime smoke proved the compiled `dist/main.js` entrypoint, non-root `node` user, liveness/readiness endpoints, and an authenticated flagged moderation response against a loopback fake MiniMax backend.
- Focused Go service and handler moderation suites passed.
- Focused service and handler race suites passed.
- `go build ./...` passed.
- `env -u OPENAI_API_KEY go test ./... -count=1` passed. Removing the variable deliberately disables an unrelated optional test that otherwise calls the real OAI token-count endpoint.
- `openspec validate add-self-hosted-qwen3guard-moderation --strict` passed.
- `git diff --check` and the scoped MiniMax-secret scan passed.

The 2026-08-03 attachment/revision source and tests add local coverage for:

- safe text plus an attachment reaching the mocked downstream path with the
  original request preserved;
- dangerous visible text plus any attachment blocking before account selection
  or upstream traffic;
- attachment-only requests producing canonical markers without an automatic
  unsafe or service-error result;
- omission of raw attachment identifiers, file names, URLs/URIs, Base64,
  headers, cookies, credentials, and bytes from MiniMax input;
- OpenAI Chat/Responses/Images, Anthropic, Gemini, Grok, and tool-traffic
  projection boundaries;
- direct structured adapter input returning a local flagged result without
  MiniMax as defense in depth;
- readiness/response classifier-revision mismatch failing closed, plus new
  projection/classifier revisions producing cache keys different from the old
  `incremental-full-context-v1` policy.

## Current-Head Local Verification

The amended 2026-08-03 local head passed the following release-shaped checks
without using production credentials or changing a deployed service:

- `env -u OPENAI_API_KEY go test ./... -count=1` and `go build ./...` passed for
  the complete backend. Removing `OPENAI_API_KEY` keeps the run offline and
  disables only the unrelated optional live OAI token-count comparison.
- Focused service and handler moderation suites and their race equivalents
  passed, including text and binary-JSON WebSocket follow-up failures, malformed
  text frames, nested attachment wrapper semantics, input-audio/data Base64
  omission, in-flight classifier-revision invalidation, provider-error cache
  ordering, and no-account/no-usage/no-upstream boundaries.
- The frontend passed 148 test files and 996 tests, typecheck, scoped lint, and a
  production build. The admin API and UI now round-trip
  `classifier_policy_revision`, so enabling the revisioned incremental cache no
  longer creates an unsaveable configuration.
- The adapter passed 7 test files and 135 tests, typecheck, and build.
- `sub2api-moderation-adapter:attachment-text-v1` was rebuilt without cache.
  A disposable loopback-backend smoke proved liveness, revisioned readiness, an
  authenticated flagged response, runtime UID `1000`, and absence of `/app/src`
  from the runtime image; the container and fake backend were then stopped.
- Strict OpenSpec validation, governance-state validation, and `git diff --check`
  passed. Independent projection/WebSocket/privacy and cache/readiness reviews
  returned no Critical or Important findings after their rejected findings were
  repaired and re-tested.

These results prove the local candidate implementation and packaging boundary.
They do not satisfy the real MiniMax classification, Windows, private-network,
isolated-candidate, rollback, or production client-boundary gates below.

The synthetic matrix covers:

- OpenAI Responses, OpenAI Chat Completions, Anthropic Messages, Gemini, image requests, and OpenAI Responses WebSocket first frames.
- Instructions, multi-turn user/assistant history, tool calls, tool results, non-user tails, and dangerous history followed by a final `Continue`.
- 12,100, 32,768, and 70,000-rune boundaries, overlap coverage, cold chunks, appended tails, and risk spanning a chunk boundary.
- Redis read/write failure, adapter failure, timeout, malformed output, the
  historical structured-image policy, 400, 402, transient retries, account
  fallback, and the no-account/no-usage/no-upstream-audit boundary.

## Privacy-Safe Real Archive Replay

The local inventory contains 59,236 prompt rows and 70,231 archived full upstream requests. Broad deterministic replay selected 1,253 representative real rows without emitting source content. A separate response-truth scan then found and replayed all 13 exact OAI `cyber_policy` cases; these executions may overlap the broad sample and are therefore reported separately rather than added as unique rows.

### Prompt corpus

- Candidates: 59,236
- Selected: 1,112
- Protocols: Chat 684, Responses 388, Gemini 20, Images 18, Anthropic 2
- Heuristic sampling pools: high-risk candidates 346, legitimate reverse/security 201, boundary candidates 196, normal/other 369
- Lengths: under 256 97, 256-1K 124, 1-4K 165, 4-12K 237, 12-32K 215, 32-64K 168, 64K+ 106
- Exact `Continue`-like prompts: 14
- Local decision-path calls: 1,523 adapter calls, 1,376 gzip requests
- Loopback fake-backend processing: P50 0.783 ms, P95 3.593 ms

### Full request archive (historical pre-amendment replay)

- Reviewable complete requests: 141
- Allowed by the loopback safe fixture: 133
- Attachment-bearing requests handled by the obsolete structured-image block
  policy: 8. This count is historical inventory only and is not acceptance
  evidence for the approved attachment pass-through V1.
- Non-image decision-path errors: 0
- Requests containing tool/function traffic: 77
- Requests containing at least 21 context items: 122
- Lengths: 32-64K 5, 64K+ 136
- Local decision-path calls: 362 adapter calls, all gzip encoded
- Ordinary local run: P50 12.764 ms, P95 148.815 ms
- Race-instrumented run: P50 84.734 ms, P95 2.178 s; the run passed without a data race

### Confirmed OAI policy truth set

- All nine complete-request archives were scanned through their response bodies; exact-token matching deliberately excludes `cyber_policy_session_blocked`.
- Exact confirmed OAI `cyber_policy` requests: 13, all OpenAI Responses requests with complete reviewable request bodies.
- Archive distribution: 2026-07-21 1, 2026-07-25 2, 2026-07-26 1, 2026-07-27 1, 2026-07-29 5, and V2 2026-07-23 3.
- Full-context local decision-path calls: 127, all gzip encoded.
- One archived request body exceeded 8 MiB (approximately 8.51 MB); the replay decoder was raised to a bounded 64 MiB test-only limit so the case is reviewed rather than silently skipped.
- The truth extraction initially assumed policy failures were HTTP 4xx, but real streaming responses can carry `cyber_policy` inside an HTTP 200 response body. The final scanner therefore uses the exact response token, not transport status.

The historical replay confirms extraction, full-context chunking, cache reuse,
gzip transport, protocol routing, and inclusion of every confirmed policy truth
case under the pre-amendment image policy. It does not prove current attachment
pass-through, attachment-content inspection, or MiniMax semantic recall. The
loopback backend intentionally returns a safe fixture, and the heuristic labels
are sampling categories rather than semantic ground truth.

## Real MiniMax Verification

The operator supplied a temporary Token Plan credential for local testing. The
credential itself was not stored in the repository, evidence, test output, or
runtime configuration files. The plan-status and model-list endpoints returned
success, and the configured account could run real classifications.

### Model comparison

- `MiniMax-M2.7-highspeed`: 9 of 10 manually labeled cases matched the expected
  decision; one neutral attachment-marker case was conservatively blocked. P50
  was 3.814 seconds and P95 was 5.490 seconds across the ten calls. Raw probes
  showed that this path emitted reasoning tokens despite the high-speed name.
- `MiniMax-M3` with thinking disabled: 10 of 10 manually labeled cases matched,
  including prompt injection, dangerous history followed by `Continue`,
  authorization bypass, DMA/evasion, credential abuse, `完全破甲`, and neutral
  attachment markers. P50 was 1.449 seconds and P95 was 2.596 seconds.
- Synthetic M3 length probes covered safe 4K, 16K, and 32K-rune inputs plus a
  32K input with dangerous text at the tail. All decisions matched; observed
  latencies ranged from 1.428 to 2.379 seconds.
- Eight concurrent cold 32K safe requests, with adapter concurrency limited to
  two, all completed but queueing raised P50 to 4.790 seconds and P95 to 8.676
  seconds.

These measurements select M3 for the current candidate, but they disprove a
strict one-second cold-request target. Prefix verdict-cache hits remain local
millisecond work; uncached provider calls must be sized and observed separately.

### Exact OAI truth set and Unicode boundary repair

- The 13 complete archived requests with an exact upstream OAI `cyber_policy`
  response were replayed through the real adapter. Their normalized lengths
  ranged from 22,581 to 703,079 Unicode code points.
- The first pass produced 10 semantic blocks and three local fail-closed errors.
  Investigation found a unit mismatch: Go chunks in Unicode code points, while
  the TypeScript adapter bounded JavaScript UTF-16 code units. Emoji and other
  non-BMP text could therefore make a valid 32,768-code-point chunk appear
  oversized.
- A red regression test proved the mismatch. The adapter now counts Unicode code
  points for both limits and redacted input-length logs. The regression test,
  adapter suite, typecheck, and build passed after the repair.
- The three affected requests were replayed again and all blocked without
  errors. The final truth-set outcome is 13 blocked, zero allowed, and zero
  errors. No original prompt text was emitted.

### Privacy-safe real prompt sample

Forty deterministic real prompt rows were replayed without printing their text:

- high-risk heuristic pool: 11 total, 9 blocked and 2 allowed in the aggregate
  run;
- boundary pool: 9 total, 5 blocked and 4 allowed;
- legitimate reverse/security pool: 8 total, 2 blocked and 6 allowed;
- normal/other pool: 12 total, 2 blocked and 10 allowed;
- provider errors: zero; aggregate P50 1.866 seconds, P95 8.262 seconds, and
  maximum 14.939 seconds.

A separate high-risk-only repeat produced 10 blocks and one allow, which exposed
classification variance under the previous temperature 0.1 setting. The only
remaining allowed row matched coarse `RCE`/code heuristics in an educational
image-course context and is not confirmed semantic ground truth. The current
policy revision therefore sets temperature zero. The complete 136-test adapter
suite, typecheck, and build passed, followed by a four-case real-provider probe:
normal text and a neutral attachment marker were allowed, while authorization
bypass and `完全破甲` prompt-injection cases were blocked. All four responses
reported `minimax-strict-policy-v4`; observed latency was 1.489 to 3.070 seconds.

### Harmless outer task with risky recent-task history

A production-shaped regression reproduces the classic wrapper observed at
19:51:43–19:51:48: the outer Codex task asks to generate zero to three
hyperpersonalized suggestions, while appended recent-task history contains a
request to bypass device-bound activation after claiming authorization. A final
benign request asks only to generate suggestions from that history.

- Sub2API's OpenAI Responses projection sent the instructions, developer record,
  risky earlier user text, and benign final user text to Moderations in order.
- The regression exposed that the non-incremental path used only local score
  thresholds and ignored the standard provider `flagged=true` bit, while the
  incremental path already combined both signals. The non-incremental path now
  uses `provider flagged OR threshold flagged`, so both paths fail closed
  consistently.
- Classifier policy `minimax-strict-policy-v5` explicitly keeps risky meaning in
  appended history in scope even when the outer task asks only for suggestions,
  summaries, recommendations, or metadata. The revision change invalidates old
  classifier-cache namespaces.
- A real M3 v5 probe blocked the risky-history case in 2.035 seconds and allowed
  the same outer wrapper with a benign weather-app history in 1.407 seconds.
- The final adapter suite passed 137 tests; the complete offline backend suite and
  build passed. No production service or configuration was changed.

## Explicit Attachment Residual Risk

V1 does not inspect image pixels or file bytes. It performs no OCR, remote
fetch, PDF/document extraction, archive or executable analysis, or malware scan.
A request whose visible text is benign but whose attachment content contains
harmful instructions can therefore pass text-only moderation and reach OAI. Tool-
extracted text becomes reviewable only when it appears in a later outbound
request context; this does not retroactively protect the first attachment
request. The adapter's direct structured-image block is a protocol guard and
must never be reported as attachment-content moderation.

## Remaining Release Blocker

This local candidate is not yet a release recommendation. Real MiniMax inference
now works, M3 is selected, and all 13 confirmed OAI risk requests blocked after
the Unicode repair. Release remains blocked because cold hosted latency exceeds
one second, the 40-row semantic sample is not the full 1,200-to-1,400 replay, and
the Token Plan is an individual interactive plan rather than a demonstrated
multi-user production capacity contract.

The amended attachment and classifier-revision behavior also remains undeployed:
no isolated-candidate or real client-boundary probe has yet proved safe attachment
pass-through, dangerous-text-plus-attachment blocking, attachment-only behavior,
or revision mismatch handling against the versioned runtime images.

The isolated production-equivalent image, rollback, and real client-boundary
gates also remain incomplete. No production configuration or traffic was changed.
Before any switch, the release gate must still measure cache-hit behavior, peak
concurrency, throttling, request amplification, and error behavior on the clean
candidate image.

## Capacity Estimate

The sampled full-context archive required 362 moderation calls for 141 requests, or approximately 2.57 provider calls per cold request. Applied to the observed scoped volume of 4,059 requests per five hours, that is an estimated 10,400 provider calls per five hours. This is a sizing estimate from one archive rather than a guaranteed production rate; the real MiniMax replay must validate cache-hit behavior and call amplification.

The currently displayed Token Plan Max reference allowance is approximately
2,250 `MiniMax-M2.7-highspeed` calls per five-hour window. That reference is not
directly equivalent to M3 usage and cannot cover the estimated all-cold call
volume. A high measured verdict-cache hit ratio or a production pay-as-you-go
capacity plan is therefore required before hosted moderation can be promoted.
