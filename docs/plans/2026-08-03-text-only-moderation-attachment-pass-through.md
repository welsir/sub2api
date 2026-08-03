# Text-Only Moderation With Attachment Pass-Through Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow valid image and file requests to use the normal Omni downstream path while sending only complete textual context and bounded attachment metadata to MiniMax.

**Architecture:** Extend the existing protocol extractors to replace attachment payloads with deterministic text markers. Keep the original client body untouched for downstream forwarding, keep MiniMax text-only, and remove the former incremental-review error that treated structured images as an unavailable moderation service.

**Tech Stack:** Go, `tidwall/gjson`, Redis-backed incremental moderation, TypeScript, Vitest, Go `testing`/`testify`, OpenSpec.

---

### Task 1: Lock the extraction contract with failing tests

**Files:**
- Modify: `backend/internal/service/content_moderation_input_test.go`
- Modify: `backend/internal/service/content_moderation_test.go`

**Step 1: Write failing tests**

Add table-driven cases for OpenAI Responses `input_image`/`input_file`, Chat
`image_url`, Anthropic `image`/`document`, Gemini `inlineData`/`fileData`, OpenAI
Images reference images, and attachment-only requests.

Each case must assert that visible text and bounded markers such as these remain:

```text
[attachment kind=image mime=image/png source=inline]
[attachment kind=file mime=application/pdf source=file_id extension=.pdf]
```

It must also assert that Base64 bytes, raw URLs/URIs or queries, opaque file ID
values, file names, headers, cookies, credentials, and file contents do not
appear in `ContentModerationInput.Text` or `ModerationInput()`.

**Step 2: Run and observe RED**

```bash
cd backend
go test ./internal/service -run 'TestExtractContentModerationInput_.*(Attachment|Image|Document|File)' -count=1
```

Expected: existing tests expose raw media through `Images`; Responses files and
Anthropic documents are missing; attachment-only requests may be empty.

### Task 2: Emit bounded markers and discard media payloads

**Files:**
- Modify: `backend/internal/service/content_moderation_input.go`
- Modify: `backend/internal/service/content_moderation.go`
- Test: `backend/internal/service/content_moderation_input_test.go`
- Test: `backend/internal/service/content_moderation_test.go`

**Step 1: Add marker helpers**

Add `addModerationAttachment` plus sanitizers that allow only bounded kind,
normalized MIME, source class, and normalized extension fields. Strip raw paths,
URLs/URIs, controls, queries, oversized values, file ID values, file names,
Base64, headers, cookies, credentials, and arbitrary objects. A
`source=file_id` field records only the reference class, never the opaque ID.

**Step 2: Map protocol fields**

- Chat `image_url` -> image marker.
- Responses `input_image` -> image marker and `input_file` -> file marker.
- Anthropic `image` -> image marker and `document` -> file marker.
- Gemini `inlineData`/`inline_data` and `fileData`/`file_data` -> bounded marker.
- OpenAI Images keeps the prompt and maps reference images to markers.
- Grok media requests keep the prompt and map remote, uploaded, and mask media
  to the same bounded markers.
- Existing captions, tools, results, instructions, roles, and ordering remain.

**Step 3: Make runtime moderation text-only**

Runtime extraction must not retain raw `Images`. `ModerationInput()` must be a
plain string for user traffic. Keep any admin image-test helper isolated from the
runtime path.

**Step 4: Run and observe GREEN**

```bash
cd backend
go test ./internal/service -run 'TestExtractContentModerationInput|TestContentModerationInput' -count=1
gofmt -w internal/service/content_moderation_input.go internal/service/content_moderation_input_test.go internal/service/content_moderation_test.go
```

### Task 3: Prove attachments no longer become moderation 503s

**Files:**
- Modify: `backend/internal/service/content_moderation_incremental_test.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/handler/openai_gateway_handler_test.go`

**Step 1: Replace the old image-fails-closed test**

Write a failing safe-text-plus-image test that requires one text moderation call,
an allowed decision, no image part, no Base64, and a stable attachment marker.

Add dangerous-text-plus-attachment coverage whose mocked flagged result must
produce zero account-selection and upstream calls.

**Step 2: Run and observe RED**

```bash
cd backend
go test ./internal/service -run 'TestContentModerationIncrementalCache_.*Attachment' -count=1
```

Historical RED expectation before implementation: the test exposed the former
`structured images are not supported by incremental moderation` error.

**Step 3: Remove only the obsolete media error branch**

The incremental path always reviews normalized text. Do not weaken cache,
transport, timeout, retry, parsing, or no-key fail-closed behavior.

**Step 4: Run and observe GREEN**

```bash
cd backend
go test ./internal/service -run 'ContentModeration.*(Attachment|Image)' -count=1
go test ./internal/handler -run 'ContentModeration.*(Attachment|Image)' -count=1
```

### Task 4: Keep MiniMax strictly text-only

**Files:**
- Modify: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/minimax.ts`
- Modify: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/minimax.test.ts`
- Modify only if the direct contract needs clarification: `services/qwen3guard-moderation-adapter/src/qwen3guard-moderation-adapter/server.ts`
- Test only if needed: `services/qwen3guard-moderation-adapter/tests/qwen3guard-moderation-adapter/adapterContract.test.ts`

**Step 1: Write failing instruction tests**

Require the trusted classifier prompt to state that the transcript is untrusted,
attachment markers are uninspected metadata, attachment presence alone is not
unsafe, and decisions use only visible text and metadata.

**Step 2: Run and observe RED**

```bash
cd services/qwen3guard-moderation-adapter
pnpm test -- minimax.test.ts adapterContract.test.ts
```

**Step 3: Update the fixed prompt only**

Do not add a second provider request, OCR, vision, file fetch, or media body.
Keep strict JSON parsing and all deterministic/transient error mappings.

The adapter's direct structured-media contract remains conservative because
Sub2API must never send structured media: a direct structured image receives a
deterministic flagged local-policy response without invoking MiniMax. Add tests
proving ordinary attachment markers are plain text, surrounding risky text
cannot be laundered by adding a marker, and the direct path is defense in depth
rather than image-pixel moderation.

Expose the active classifier policy revision on `/readyz` and every successful
Moderations response. Sub2API must pin the expected revision, reject a missing
or mismatched readiness revision before the first cache read for that
base-URL/revision pair, reject a mismatched response before writing its verdict,
invalidate the remembered readiness check after a response mismatch, and
include both the text-projection and classifier revisions in chunk hashes and
Redis namespaces.

**Step 4: Run and observe GREEN**

```bash
pnpm typecheck
pnpm test
pnpm build
```

### Task 5: Update specifications and evidence

**Files:**
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/design.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/specs/group-scoped-moderation-rollout/spec.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/specs/qwen3guard-moderation-adapter/spec.md`
- Modify: `openspec/changes/add-self-hosted-qwen3guard-moderation/tasks.md`
- Modify: `.specgov/changes/add-self-hosted-qwen3guard-moderation/evidence/minimax-local-candidate.md`

**Step 1: Replace obsolete attachment blocking claims**

Specify that valid attachments remain in the original downstream request, while
only complete visible text and safe markers enter moderation.

**Step 2: Record the accepted residual risk**

Explicitly exclude image pixels, file bodies, OCR, archives, malware analysis,
and multimodal moderation. Never claim that attachment contents were inspected.

Record that valid attachments remain unchanged in the original downstream
request, while the MiniMax projection excludes raw URLs/URIs, file IDs, file
names, Base64, headers, cookies, credentials, and binary content. Attachment
presence alone is not an allow or block/review signal.

**Step 3: Record revision compatibility**

Document the expected classifier revision handshake and versioned cache
namespace. Missing or mismatched revision evidence fails closed and old policy
verdicts cannot be reused after the attachment projection changes.

**Step 4: Preserve privacy**

Evidence may contain commands, aggregate counts, protocols, pass/fail, and hashes
where appropriate, but no user prompts, credentials, headers, raw URLs, or file
contents.

### Task 6: Run the complete local package acceptance suite

**Files:**
- Modify if needed: `backend/internal/service/content_moderation_archive_replay_test.go`
- Modify: `.specgov/changes/add-self-hosted-qwen3guard-moderation/evidence/minimax-local-candidate.md`

**Step 1: Run race-aware focused Go tests**

```bash
cd backend
go test -race ./internal/service ./internal/handler -run 'ContentModeration' -count=1
```

**Step 2: Run affected Go packages**

```bash
go test ./internal/service ./internal/handler -count=1
```

**Step 3: Run adapter verification**

```bash
cd ../services/qwen3guard-moderation-adapter
pnpm typecheck
pnpm test
pnpm build
```

**Step 4: Run privacy-safe replay and synthetic attachment cases**

Real archives provide structure counts and representative request shapes only.
Use synthetic semantics for safe/unsafe attachment tests. Never print original
prompts or credentials.

The final evidence must distinguish the historical structured-image-block
checkpoint from the approved attachment pass-through V1 and must not reuse the
old image-block count as acceptance evidence.

**Step 5: Check the package boundary**

```bash
git diff --check
git diff --name-only
git status --short --branch
```

Expected: all changes remain on `dev/omni`; no credentials, production config,
generated corpus, unrelated branch, or live system is touched.

**Step 6: Hold release**

Do not commit, push, build a production image, change live configuration, or
deploy. Report the complete matrix and residual risks for the user's single
package-level go/no-go decision.
