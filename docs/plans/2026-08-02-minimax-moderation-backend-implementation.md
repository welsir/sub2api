# MiniMax Moderation Backend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a strict MiniMax Chat Completions backend behind the existing OpenAI-shaped moderation adapter without changing Sub2API's moderation contract or Qwen compatibility.

**Architecture:** Keep `POST /v1/moderations` as the gateway boundary and select a provider-specific backend client from validated configuration. MiniMax uses a fixed classifier instruction, maps provider-sensitive signals directly to a blocked classification, and exposes deterministic auth/billing failures as non-retryable 4xx responses.

**Tech Stack:** TypeScript, Node.js HTTP/fetch, Vitest, Go, OpenSpec.

---

### Task 1: Add Provider Configuration

**Files:**
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/config.ts`
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/config.js`
- Test: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/config.test.ts`

**Step 1:** Write failing tests proving the compatibility default is `qwen`,
`minimax` is accepted, unknown providers are rejected, and MiniMax defaults
readiness to `/v1/models` while Qwen keeps `/health`.

**Step 2:** Run `pnpm vitest run tests/qwen3guard-moderation-adapter/config.test.ts`.
Expected: RED because `backendProvider` does not exist.

**Step 3:** Add `BackendProvider = "qwen" | "minimax"`, validate
`QWEN3GUARD_BACKEND_PROVIDER`, store it on `AdapterConfig`, and select the
provider-specific readiness default. Mirror the runtime JavaScript source.

**Step 4:** Rerun the focused test. Expected: GREEN.

### Task 2: Add Strict MiniMax Classification Mapping

**Files:**
- Create: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/minimax.ts`
- Create: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/minimax.js`
- Create: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/minimax.test.ts`
- Modify: source and test `.folder.md` files in the adapter.

**Step 1:** Write failing tests for strict `allow`, `block`, and `review`;
`<think>` wrappers; unknown decisions; malformed/truncated JSON; and injected
`allow` text before the final decision.

**Step 2:** Run `pnpm vitest run tests/qwen3guard-moderation-adapter/minimax.test.ts`.
Expected: RED with module not found.

**Step 3:** Export a fixed classifier instruction, a request builder, a strict
final-JSON parser, and sensitive-result mapping. Accept only:

```json
{"decision":"allow|block|review","category":"...","confidence":0.0,"reason_code":"..."}
```

Only `allow` maps to `Safe`. `block` and `review` map to `Unsafe` and the existing
`illicit` category. Unknown or incomplete objects throw. Add a MiniMax-specific
mapping revision and mirror the runtime JavaScript source.

**Step 4:** Rerun the focused test. Expected: GREEN.

### Task 3: Implement Provider-Neutral Backend Clients

**Files:**
- Modify: adapter `backend.ts` and `backend.js`.
- Test: adapter `adapterContract.test.ts`.

**Step 1:** Add a fake MiniMax backend and failing tests proving separate
system/user messages, bearer auth, non-streaming output, bounded completion
tokens, and the configured model. Cover sensitive flags, codes `1026`/`1027`,
auth `1004`, balance `1008`, throttling, malformed output, timeout, and oversized
responses.

**Step 2:** Run the adapter contract test. Expected: RED because only the Qwen
client exists.

**Step 3:** Add a `ModerationBackendClient` interface and
`createBackendClient(config, fetchImpl)`. Preserve Qwen behavior. Implement a
MiniMax client that reads bounded response bodies even on provider errors and
maps:

- `1026`, `1027`, or sensitive flags to a successful blocked classification;
- `1004` or HTTP 401/403 to typed `auth` failure;
- `1008` or HTTP 402 to typed `billing` failure;
- `1001`, `1002`, `1024`, `1033`, HTTP 408/429/5xx to retryable backend failure.

Use `GET /v1/models` for MiniMax readiness. Never log credentials or raw
transcripts. Mirror runtime JavaScript.

**Step 4:** Rerun the contract test. Expected: Qwen and MiniMax scenarios pass.

### Task 4: Wire Deterministic Failure Semantics

**Files:**
- Modify: adapter `server.ts`, `server.js`, `metrics.ts`, and `metrics.js`.
- Test: adapter contract tests and `backend/internal/service/content_moderation_test.go`.

**Step 1:** Add failing HTTP-boundary tests asserting provider auth returns 401,
billing returns 402, parse/transient failures remain retryable 5xx, and sensitive
outcomes return a normal flagged Moderations response.

**Step 2:** Run focused TypeScript and Go content-moderation tests. Expected: RED
for the new provider cases.

**Step 3:** Construct the backend through the factory, emit `backend_provider`
and the result mapping revision, and map deterministic errors to 401/402. Retain
generic client messages and failure-closed behavior. Do not add automatic
user/key banning.

**Step 4:** Rerun focused tests. Expected: GREEN.

### Task 5: Document And Validate The Rollout Contract

**Files:**
- Modify: adapter `.env.example`, `README.md`, and `.folder.md` files.
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/design.md`.
- Modify: matching OpenSpec tasks and adapter specification.

**Step 1:** Document the provider setting, `/v1/models` readiness, M2.7
configuration, sensitive/error mapping, dedicated secret, high-risk group scope,
no automatic ban, and credentialed offline replay requirement.

**Step 2:** Run `pnpm test` and `pnpm typecheck` in the adapter. Expected: PASS.

**Step 3:** Run focused Go service, handler, and admin moderation tests. Expected:
PASS.

**Step 4:** Run strict OpenSpec validation, `git diff --check`, and inspect Git
status/name-only. Expected: only approved `dev/omni` files are modified.

**Step 5:** Commit as `feat(omni): add MiniMax moderation backend`.
