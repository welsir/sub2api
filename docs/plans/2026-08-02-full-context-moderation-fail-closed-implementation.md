# Full-Context Moderation Fail-Closed Implementation Plan

> **For Codex:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ensure every scoped Omni outbound request is moderated with its complete semantic context and never reaches the downstream account after a final moderation failure.

**Architecture:** Keep policy in `ContentModerationService`, protocol extraction in `content_moderation_input.go`, and HTTP adaptation in the standalone Qwen3Guard adapter. Use stable role-tagged transcripts, gzip for large Moderations payloads, three total retry attempts for transient failures, and a local 503 decision for `pre_block` failure.

**Tech Stack:** Go, GJSON, Gin, Node.js HTTP/zlib, TypeScript, Vitest, OpenSpec.

---

### Task 1: Update the governed contract

**Files:**
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/proposal.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/design.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/tasks.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/specs/group-scoped-moderation-rollout/spec.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/specs/qwen3guard-moderation-adapter/spec.md`

Record full-context extraction, no silent truncation, gzip transport, three total attempts, `observe` fail-open, and scoped `pre_block` fail-closed semantics. Run `openspec validate add-self-hosted-qwen3guard-moderation --strict`.

### Task 2: Replace last-message-only extraction

**Files:**
- Modify: `backend/internal/service/content_moderation_input_test.go`
- Modify: `backend/internal/service/content_moderation_test.go`
- Modify: `backend/internal/service/content_moderation_input.go`
- Modify: `backend/internal/service/content_moderation.go`

Write failing protocol tests for complete history, tool calls/outputs, `Continue`, stable prefixes, and text beyond 12,000 characters. Implement ordered role-tagged extraction and remove silent text truncation. Run focused service tests.

### Task 3: Fail closed after bounded retries

**Files:**
- Modify: `backend/internal/service/content_moderation_test.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/handler/content_moderation_helper.go`
- Modify: handler moderation tests as needed

Write failing tests proving one Key receives three transient attempts, final failure returns a blocked 503 decision in `pre_block`, missing Keys are blocked, and handler errors cannot return the allow path. Implement the minimum policy change while preserving `observe` behavior.

### Task 4: Compress the moderation transport

**Files:**
- Modify: `backend/internal/service/content_moderation_test.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/adapterContract.test.ts`
- Modify: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/runtime.test.js`
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/server.ts`
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/server.js`

Write failing Go and adapter tests for gzip request/acceptance, malformed streams, and decompressed-size limits. Implement best-speed gzip above 1 KiB and bounded adapter decompression.

### Task 5: Synchronize operator documentation and verify

**Files:**
- Modify: `services/qwen3guard-moderation-adapter/README.md`
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/.folder.md`
- Modify: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/.folder.md`
- Modify: `services/qwen3guard-moderation-adapter/.env.example` only if bounds change

Run Go focused tests, adapter tests/typecheck, OpenSpec strict validation, `git diff --check`, and inspect `git diff --name-only`. Do not claim the one-second latency target until the Windows 5070 Ti warm-path benchmark is available.
