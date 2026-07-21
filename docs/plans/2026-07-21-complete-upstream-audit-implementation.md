# Complete Upstream Audit Implementation Plan

> **For Codex:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist and archive exact upstream request/response attempts on `dev/omni` with asynchronous fail-open behavior.

**Architecture:** Attach identity metadata at the handler, capture HTTP at the shared upstream transport, capture OpenAI WS at its write/read loop, store zstd-compressed bytes in a daily-partitioned PostgreSQL table, and archive/drop yesterday's verified partition.

**Tech Stack:** Go, PostgreSQL 18, zstd, sqlmock/testify, Python 3, unittest, gzip/CSV, Codex cron automation.

---

### Task 1: Define audit contract and compression

**Files:**
- Modify: `backend/internal/service/prompt_audit.go`
- Create: `backend/internal/service/upstream_audit_capture.go`
- Test: `backend/internal/service/prompt_audit_test.go`
- Test: `backend/internal/service/upstream_audit_capture_test.go`

1. Write failing tests for metadata context, zstd round-trip, SHA256/sizes, request capture, response EOF/close/error, exact SSE bytes, overflow, and queue-full fail-open.
2. Run the targeted tests and confirm they fail for missing full-attempt behavior.
3. Implement the smallest bounded capture and asynchronous recorder that satisfies the tests.
4. Re-run the tests until green and format the files.

### Task 2: Add partitioned persistence

**Files:**
- Create: `backend/migrations/175_upstream_audit_logs.sql`
- Modify: `backend/migrations/migrations.go`
- Modify: `backend/migrations/prompt_audit_migration_test.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify: `backend/internal/repository/prompt_audit_repo.go`
- Modify: `backend/internal/repository/prompt_audit_repo_test.go`

1. Write failing migration and repository tests for the daily-partitioned schema, partition helper, and complete row fields.
2. Run the target tests and verify the expected failures.
3. Add migration 175 and repository batch persistence with partition ensure.
4. Re-run target tests and migration integration checks.

### Task 3: Capture real HTTP attempts

**Files:**
- Modify: `backend/internal/handler/content_moderation_helper.go`
- Modify: `backend/internal/handler/prompt_audit_helper_test.go`
- Modify: `backend/internal/repository/http_upstream.go`
- Modify: `backend/internal/repository/http_upstream_test.go`

1. Write failing tests proving handler metadata reaches request context and the shared HTTP transport records final request bytes, response bytes, failures, and independent retries.
2. Run and verify RED.
3. Replace legacy extracted-prompt recording with audit-context attachment and wrap the actual `client.Do` result.
4. Re-run and verify GREEN.

### Task 4: Capture OpenAI WebSocket attempts

**Files:**
- Modify: `backend/internal/service/openai_ws_forwarder_v2.go`
- Create or modify: `backend/internal/service/openai_ws_forwarder_audit_test.go`

1. Write failing tests for final WS payload and raw message JSONL aggregation on success and error.
2. Run and verify RED.
3. Add the same bounded capture around WS write/read without changing forwarding behavior.
4. Re-run and verify GREEN.

### Task 5: Upgrade daily archive and analysis

**Files:**
- Modify: `/Users/welsir/Downloads/账号凭证/用户prompt/export_previous_day.py`
- Modify: `/Users/welsir/Downloads/账号凭证/用户prompt/test_export_previous_day.py`

1. Write failing Python tests for the new header, partition naming, exact drop-after-verification gate, zstd decoding, analysis outputs, resume behavior, and failure protection.
2. Run and verify RED.
3. Implement deterministic export, manifest v2, verified partition drop, and local analysis.
4. Re-run and verify GREEN without contacting production.

### Task 6: Update automation and operational documentation

**Files:**
- Update Codex automation `omni-prompt` through the automation API.
- Create: `docs/governance/runs/omni-complete-upstream-audit.md`

1. Update the automation prompt to run the fixed script, allow analysis, and prohibit bypassing checks or manual deletion.
2. Preserve the daily 09:00 schedule and existing model/settings unless required.
3. Record commands, test results, storage estimate, release plan, and remaining risks in the run log.

### Task 7: Final verification and delivery

1. Run targeted Go tests, Python tests, migration checks, formatting, and the relevant broader backend suite.
2. Inspect `git diff --check`, `git diff --name-only`, and `git status --short` in `dev/omni`.
3. Confirm external script and automation state separately from Git.
4. Stop with the full diff and verification evidence for user review. Do not commit, push, create a PR, or deploy production.
