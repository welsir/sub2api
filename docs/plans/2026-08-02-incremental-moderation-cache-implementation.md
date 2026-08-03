# Incremental Full-Context Moderation Cache Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Review every visible transcript character while sending only uncached, deterministic text chunks to MiniMax and preserving a fail-closed pre-account-selection boundary.

**Architecture:** Sub2API deterministically splits normalized transcripts into overlapping chunks, reads versioned verdicts from its existing Redis connection, and submits only misses to the existing `/v1/moderations` adapter with bounded parallelism and one overall deadline. The adapter remains stateless and converts MiniMax uncertainty or malformed classifier output into a normal unsafe result so large requests are not retried solely because the model omitted strict JSON.

**Tech Stack:** Go, Redis, `golang.org/x/sync/errgroup`, TypeScript, Node.js HTTP/fetch, Vitest, Vue 3, OpenSpec, Docker.

---

### Task 1: Add An Explicit Incremental-Cache Rollout Switch

**Files:**
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`
- Modify: `backend/internal/service/content_moderation_test.go`
- Modify: `frontend/src/api/admin/riskControl.ts`
- Modify: `frontend/src/views/admin/RiskControlView.vue`
- Modify: `frontend/src/views/admin/__tests__/RiskControlView.spec.ts`

**Step 1: Write failing backend configuration tests**

Add tests proving `incremental_cache_enabled` defaults to false, is accepted by
the update contract, survives normalization/cloning, and is present in the safe
admin view. This preserves an explicit rollback switch.

**Step 2: Run the focused backend tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler/admin -run 'ContentModeration.*Incremental|Incremental.*ContentModeration' -count=1
```

Expected: FAIL because the field does not exist.

**Step 3: Add the minimal backend field wiring**

Add `IncrementalCacheEnabled bool` to `ContentModerationConfig` and its view,
`*bool` to `UpdateContentModerationConfigInput`, copy it in update/view/clone
paths, and expose the matching handler request field. Keep the default false.

**Step 4: Add the frontend toggle and failing/passing view test**

Add the API types, form default/load/save mapping, and one advanced safety toggle
labelled as incremental full-context cache. Run:

```bash
pnpm --dir frontend vitest run src/views/admin/__tests__/RiskControlView.spec.ts
```

Expected after implementation: PASS.

**Step 5: Run focused backend tests and commit**

```bash
cd backend && go test ./internal/service ./internal/handler/admin -run 'ContentModeration' -count=1
git add backend/internal/service/content_moderation.go backend/internal/service/content_moderation_test.go backend/internal/handler/admin/content_moderation_handler.go frontend/src/api/admin/riskControl.ts frontend/src/views/admin/RiskControlView.vue frontend/src/views/admin/__tests__/RiskControlView.spec.ts
git commit -m "feat(omni): add incremental moderation rollout switch"
```

### Task 2: Implement Deterministic Unicode Chunking

**Files:**
- Create: `backend/internal/service/content_moderation_chunk.go`
- Create: `backend/internal/service/content_moderation_chunk_test.go`

**Step 1: Write failing table-driven tests**

Cover empty/short/exact-boundary/multi-window Unicode input, exact 32,768-rune
windows, 1,024-rune overlap, total code-point coverage, stable prefix hashes
after appending text, and policy namespace changes for model/base URL/threshold
changes.

Also cover the attachment text-projection revision and expected classifier
policy revision so either change produces new hashes and namespaces instead of
reusing stale verdicts.

**Step 2: Run the pure helper tests**

```bash
cd backend && go test ./internal/service -run 'ContentModerationChunk' -count=1
```

Expected: FAIL because the helper does not exist.

**Step 3: Implement the minimal pure helpers**

Add constants for chunk size, overlap, parallelism, safe TTL, block TTL, and
revision. Implement rune-safe windows, SHA-256 chunk IDs, and a canonical policy
namespace built from the fixed revision, normalized base URL, model, and sorted
threshold keys. Do not store text in identifiers.

**Step 4: Rerun tests and commit**

```bash
cd backend && go test ./internal/service -run 'ContentModerationChunk' -count=1
git add backend/internal/service/content_moderation_chunk.go backend/internal/service/content_moderation_chunk_test.go
git commit -m "feat(omni): split moderation context into stable chunks"
```

### Task 3: Add A Versioned Redis Verdict Cache

**Files:**
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/repository/content_moderation_hash_cache.go`
- Create: `backend/internal/repository/content_moderation_hash_cache_test.go`

**Step 1: Define the cache contract and failing repository tests**

Introduce an exported compact result containing `Flagged` and category scores,
plus batch get/set methods keyed by namespace and chunk hash. Using miniredis,
test misses, safe/block round trips, per-verdict TTLs, namespace isolation,
corrupt-value-as-miss, and Redis errors.

**Step 2: Run the repository test**

```bash
cd backend && go test ./internal/repository -run 'ContentModerationChunkCache' -count=1
```

Expected: FAIL because batch methods are missing.

**Step 3: Implement Redis MGET and pipelined SET**

Use keys shaped as
`content_moderation:chunk:<namespace-hash>:<chunk-hash>`. Validate both hashes as
lowercase SHA-256 before Redis access. Store JSON verdicts only, never prompt
text. Treat malformed cached JSON as a miss; propagate Redis command failures.

**Step 4: Rerun tests and commit**

```bash
cd backend && go test ./internal/repository -run 'ContentModerationChunkCache' -count=1
git add backend/internal/service/content_moderation.go backend/internal/repository/content_moderation_hash_cache.go backend/internal/repository/content_moderation_hash_cache_test.go
git commit -m "feat(omni): cache moderation chunk verdicts in Redis"
```

### Task 4: Review Only Cache Misses Under One Deadline

**Files:**
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/service/content_moderation_test.go`
- Modify: `backend/internal/service/content_moderation_input_test.go`

**Step 1: Add failing orchestration tests**

Use an `httptest.Server` and cache fake to prove:

- every cold chunk is submitted and strict allows are cached;
- a repeated appended transcript submits only the changed tail chunk(s);
- a cached unsafe chunk blocks without an HTTP call;
- dangerous history followed by `Continue` blocks;
- Redis read/write failure, provider failure, and deadline expiration return the
  local moderation failure decision;
- safe text plus attachments and attachment-only requests remain on the
  text-only moderation path without automatic failure;
- dangerous visible text plus an attachment blocks without OAI selection;
- a missing or mismatched readiness classifier revision fails closed before the
  first cache read for that base-URL/revision pair, while a mismatched response
  fails closed before verdict write and invalidates the remembered readiness
  check;
- disabling the switch preserves the legacy single-request path.

**Step 2: Run the focused tests**

```bash
cd backend && go test ./internal/service -run 'ContentModeration.*(Chunk|Incremental|Continue)' -count=1
```

Expected: FAIL because incremental orchestration is missing.

**Step 3: Implement bounded miss review**

Type-assert the production hash cache to the new chunk-cache interface in the
service constructor. When the switch is enabled, require that interface, create
one `context.WithTimeout` using `cfg.TimeoutMS`, batch-read verdicts, block on
any cached unsafe result, and review misses through `errgroup` with limit 8.
Cancel remaining work on unsafe/error where possible. Cache only strict provider
results; any required cache failure returns the existing local 503 decision.

Require `classifier_policy_revision` in configuration, verify the same value
from adapter `/readyz` before cache access, and require it on every successful
Moderations response before writing a verdict. Include both the attachment
text-projection revision and classifier-policy revision in chunk hashes and
Redis namespaces.

Merge category scores by maximum score so the existing threshold evaluation and
logging path stays authoritative. Emit only aggregate slog fields for total
chunks, hits, misses, reviewed chunks, outcome, and latency.

**Step 4: Run service and handler regression tests**

```bash
cd backend && go test ./internal/service -run 'ContentModeration' -count=1
cd backend && go test ./internal/handler -run 'ContentModeration' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/service/content_moderation.go backend/internal/service/content_moderation_test.go backend/internal/service/content_moderation_input_test.go
git commit -m "feat(omni): review only uncached moderation chunks"
```

### Task 5: Convert MiniMax Classifier Uncertainty Into A Local Block

**Files:**
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/minimax.ts`
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/minimax.js`
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/backend.ts`
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/backend.js`
- Modify: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/minimax.test.ts`
- Modify: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/minimaxBackend.test.ts`
- Modify: adapter source/test `.folder.md` and `README.md`

**Step 1: Add failing MiniMax-only tests**

Prove missing choices, non-stop finish reasons, empty content, and malformed or
incomplete classifier JSON return an `Unsafe/illicit` moderation result.
Preserve Qwen parse-error behavior and preserve retryable network/HTTP/invalid-
provider-body failures.

**Step 2: Run focused adapter tests**

```bash
pnpm --dir services/qwen3guard-moderation-adapter vitest run tests/qwen3guard-moderation-adapter/minimax.test.ts tests/qwen3guard-moderation-adapter/minimaxBackend.test.ts
```

Expected: FAIL because malformed MiniMax outputs currently throw parse errors.

**Step 3: Implement one uncertain-result mapper**

Map classifier-output uncertainty to the same strict `review -> Unsafe` result.
Do not catch transport, auth, billing, invalid provider JSON, timeout, or caller
cancellation. Mirror the checked-in runtime JavaScript.

**Step 4: Run adapter tests/typecheck and commit**

```bash
pnpm --dir services/qwen3guard-moderation-adapter test
pnpm --dir services/qwen3guard-moderation-adapter typecheck
git add services/qwen3guard-moderation-adapter
git commit -m "fix(omni): block uncertain MiniMax classifications"
```

### Task 6: Update The Governed Contract And Operator Documentation

**Files:**
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/design.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/tasks.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/specs/qwen3guard-moderation-adapter/spec.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/specs/group-scoped-moderation-rollout/spec.md`
- Modify: `services/qwen3guard-moderation-adapter/README.md`

**Step 1: Replace old full-resend assumptions**

Document deterministic chunk coverage, Redis hash-only verdicts, policy
namespaces, TTLs, one overall deadline, malformed-result blocking, rollout
switch, aggregate observability, attachment text-only pass-through, direct-
adapter structured-media defense, classifier-revision handshake, and fail-closed
cache behavior.

**Step 2: Validate documentation and OpenSpec**

Run the repository's existing OpenSpec validation command discovered from
`package.json` or existing scripts, followed by `git diff --check`.

Expected: PASS with no stale statement saying every turn must resend the whole
transcript to MiniMax.

**Step 3: Commit**

```bash
git add openspec/changes/add-self-hosted-qwen3guard-moderation services/qwen3guard-moderation-adapter/README.md
git commit -m "docs(omni): specify incremental moderation review"
```

### Task 7: Full Verification And Isolated Rollout

**Files:**
- No new source files expected.
- Production mutations only after all local checks pass.

**Step 1: Run local verification**

```bash
cd backend && go test ./internal/service ./internal/repository ./internal/handler ./internal/handler/admin -count=1
pnpm --dir frontend vitest run src/views/admin/__tests__/RiskControlView.spec.ts
pnpm --dir services/qwen3guard-moderation-adapter test
pnpm --dir services/qwen3guard-moderation-adapter typecheck
git diff --check
git status --short --branch
```

Expected: all task-relevant suites pass and the worktree is clean after commits.

**Step 2: Build versioned application and adapter images locally**

Tag images with the source commit. Scan the build context and image metadata for
the MiniMax key prefix; expected result is no match.

**Step 3: Start isolated production candidates**

Transfer images through the audited SSH path, load them without replacing the
current containers, and start isolated candidate ports/networks where possible.
Do not restart PostgreSQL or Redis.

**Step 4: Run fail-closed canaries**

Verify a cold multi-chunk transcript, the same transcript with an appended
`Continue`, a high-risk tail, safe text plus an attachment, attachment-only
input, dangerous text plus an attachment, direct structured adapter input,
revision mismatch, Redis unavailability, and MiniMax unavailability. For every
blocked/error canary assert `usage_rows=0` and `account_ids=none`; do not claim
that attachment contents were inspected.

**Step 5: Switch with rollback ready**

Back up Compose and moderation JSON, replace only the versioned application and
adapter targets, enable `incremental_cache_enabled`, and monitor latency/cache
hit/error aggregates. If health, cache correctness, or no-OAI assertions fail,
restore the prior Compose/config immediately.

**Step 6: Final branch and deployment audit**

Confirm the branch is `dev/omni`, list changed files/commits, confirm no other
branch was modified or pushed, and retain redacted rollback evidence.
