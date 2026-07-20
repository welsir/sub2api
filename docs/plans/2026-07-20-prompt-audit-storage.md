# Prompt Audit Storage Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist the latest user-authored text from every eligible Omni request into PostgreSQL without blocking or changing the upstream model request.

**Architecture:** Add an append-only `prompt_audit_logs` table and a dedicated prompt-audit service with a bounded, non-blocking in-process queue. Reuse the existing authenticated handler seam that already receives protocol, model, raw body, user, API key, group, endpoint, and request ID; keep extraction and persistence separate from billing and content moderation.

**Tech Stack:** Go, Gin, PostgreSQL migrations, `database/sql`, GJSON, Google Wire, `sqlmock`, Go testing.

---

### Task 1: Add the append-only audit table

**Files:**
- Create: `backend/migrations/174_prompt_audit_logs.sql`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`

**Step 1: Write the failing schema integration expectations**

Add assertions requiring `prompt_audit_logs` and its core columns:

```go
requireColumn(t, tx, "prompt_audit_logs", "request_id", "character varying", 64, false)
requireColumn(t, tx, "prompt_audit_logs", "user_id", "bigint", 0, false)
requireColumn(t, tx, "prompt_audit_logs", "api_key_id", "bigint", 0, false)
requireColumn(t, tx, "prompt_audit_logs", "group_id", "bigint", 0, true)
requireColumn(t, tx, "prompt_audit_logs", "endpoint", "character varying", 128, false)
requireColumn(t, tx, "prompt_audit_logs", "protocol", "character varying", 64, false)
requireColumn(t, tx, "prompt_audit_logs", "model", "character varying", 100, false)
requireColumn(t, tx, "prompt_audit_logs", "prompt_text", "text", 0, false)
requireColumn(t, tx, "prompt_audit_logs", "prompt_chars", "integer", 0, false)
requireColumn(t, tx, "prompt_audit_logs", "created_at", "timestamp with time zone", 0, false)
```

**Step 2: Run the schema integration test and verify it fails**

Run:

```bash
cd backend
go test ./internal/repository -run TestMigrationsSchema -count=1
```

Expected: FAIL because `prompt_audit_logs` does not exist.

**Step 3: Add the idempotent migration**

Create `backend/migrations/174_prompt_audit_logs.sql`:

```sql
CREATE TABLE IF NOT EXISTS prompt_audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    request_id   VARCHAR(64) NOT NULL DEFAULT '',
    user_id      BIGINT NOT NULL,
    api_key_id   BIGINT NOT NULL,
    group_id     BIGINT,
    endpoint     VARCHAR(128) NOT NULL DEFAULT '',
    protocol     VARCHAR(64) NOT NULL DEFAULT '',
    model        VARCHAR(100) NOT NULL DEFAULT '',
    prompt_text  TEXT NOT NULL,
    prompt_chars INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_created_at
    ON prompt_audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_user_created_at
    ON prompt_audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_api_key_created_at
    ON prompt_audit_logs(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_request_id
    ON prompt_audit_logs(request_id);
```

Do not add foreign keys: audit rows must survive later user/API-key deletion.

**Step 4: Run the schema test and migration checks**

Run:

```bash
cd backend
go test ./internal/repository -run TestMigrationsSchema -count=1
go test ./migrations -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/migrations/174_prompt_audit_logs.sql backend/internal/repository/migrations_schema_integration_test.go
git commit -m "feat(omni): add prompt audit log table"
```

### Task 2: Extract only the latest user-authored Prompt

**Files:**
- Create: `backend/internal/service/prompt_audit_input.go`
- Create: `backend/internal/service/prompt_audit_input_test.go`

**Step 1: Write failing table-driven extraction tests**

Cover these cases:

```go
tests := []struct {
    name     string
    protocol string
    body     string
    want     string
}{
    {
        name: "responses stores latest user message",
        protocol: ContentModerationProtocolOpenAIResponses,
        body: `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"second\nline"}]}]}`,
        want: "second\nline",
    },
    {
        name: "chat stores latest user message",
        protocol: ContentModerationProtocolOpenAIChat,
        body: `{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"second"}]}`,
        want: "second",
    },
    {
        name: "tool continuation is skipped",
        protocol: ContentModerationProtocolOpenAIResponses,
        body: `{"input":[{"type":"function_call_output","call_id":"call_1","output":"result"}]}`,
        want: "",
    },
    {
        name: "image stores prompt but not image bytes",
        protocol: ContentModerationProtocolOpenAIImages,
        body: `{"prompt":"draw a moon","image":"data:image/png;base64,AAAA"}`,
        want: "draw a moon",
    },
}
```

Also test Anthropic, Gemini, multi-block text order, invalid JSON, empty text, assistant-last, and preservation of whitespace inside the latest user text.

**Step 2: Run the focused test and verify it fails**

Run:

```bash
cd backend
go test ./internal/service -run TestExtractLatestUserPrompt -count=1
```

Expected: FAIL because `ExtractLatestUserPrompt` does not exist.

**Step 3: Implement the protocol-specific extractor**

Add:

```go
func ExtractLatestUserPrompt(protocol string, body []byte) string
```

Implementation rules:

- Validate JSON with GJSON.
- Inspect only the final semantic item in the protocol's user-message collection.
- Require `role=user` when the protocol supplies roles.
- Accept string content and ordered `text`/`input_text` blocks.
- Join multiple text blocks with `\n` without applying `strings.Fields` or the moderation 12,000-rune cap.
- Return empty for assistant messages, tool calls, tool results, images/files without text, or invalid JSON.
- For image generation, return the top-level `prompt` string.
- Never collect image URLs, Base64, file payloads, system/developer content, assistant content, or tool content.

Keep this extractor independent from `ExtractContentModerationInput`; changing moderation normalization would broaden the feature beyond scope.

**Step 4: Run focused service tests**

Run:

```bash
cd backend
go test ./internal/service -run 'TestExtractLatestUserPrompt|TestExtractContentModerationInput' -count=1
```

Expected: PASS, including existing moderation extraction tests.

**Step 5: Commit**

```bash
git add backend/internal/service/prompt_audit_input.go backend/internal/service/prompt_audit_input_test.go
git commit -m "feat(omni): extract latest user prompt for audit"
```

### Task 3: Add repository and bounded fail-open queue

**Files:**
- Create: `backend/internal/service/prompt_audit.go`
- Create: `backend/internal/service/prompt_audit_test.go`
- Create: `backend/internal/repository/prompt_audit_repo.go`
- Create: `backend/internal/repository/prompt_audit_repo_test.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/repository/wire.go`

**Step 1: Write failing repository tests**

Define the semantic record and port in the service package:

```go
type PromptAuditLog struct {
    ID          int64
    RequestID   string
    UserID      int64
    APIKeyID    int64
    GroupID     *int64
    Endpoint    string
    Protocol    string
    Model       string
    PromptText  string
    PromptChars int
    CreatedAt   time.Time
}

type PromptAuditRepository interface {
    Create(ctx context.Context, log *PromptAuditLog) error
}
```

Use `sqlmock` to verify the repository inserts all ten persisted fields, handles nullable `group_id`, scans `id`/`created_at`, and wraps database errors.

**Step 2: Run repository tests and verify they fail**

Run:

```bash
cd backend
go test ./internal/repository -run TestPromptAuditRepository -count=1
```

Expected: FAIL because `NewPromptAuditRepository` is missing.

**Step 3: Implement the repository**

Add `NewPromptAuditRepository(db *sql.DB) service.PromptAuditRepository` and execute:

```sql
INSERT INTO prompt_audit_logs (
    request_id, user_id, api_key_id, group_id, endpoint,
    protocol, model, prompt_text, prompt_chars
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id, created_at
```

Register `NewPromptAuditRepository` in `backend/internal/repository/wire.go`.

**Step 4: Write failing queue/service tests**

Define:

```go
type PromptAuditInput struct {
    RequestID string
    UserID    int64
    APIKeyID  int64
    GroupID   *int64
    Endpoint  string
    Protocol  string
    Model     string
    Body      []byte
}

func NewPromptAuditService(repo PromptAuditRepository) *PromptAuditService
func (s *PromptAuditService) Record(input PromptAuditInput) bool
```

Tests must prove:

- a user-bearing request is enqueued and persisted;
- `PromptChars` uses Unicode rune count;
- a tool continuation produces no record;
- queue-full returns `false` immediately;
- repository failure is logged/absorbed and never returned to the caller;
- the service copies request data needed by the worker and does not retain request context.

Use an unexported test constructor such as:

```go
func newPromptAuditService(repo PromptAuditRepository, queueSize, workerCount int) *PromptAuditService
```

to make queue-full and worker behavior deterministic.

**Step 5: Implement the bounded asynchronous service**

Use a fixed production queue and worker count:

```go
const (
    defaultPromptAuditQueueSize   = 4096
    defaultPromptAuditWorkerCount = 2
    promptAuditWriteTimeout       = 5 * time.Second
)
```

`Record` extracts synchronously and uses a non-blocking enqueue:

```go
select {
case s.queue <- log:
    return true
default:
    slog.Warn("prompt_audit.queue_full", "request_id", input.RequestID)
    return false
}
```

Each worker creates a fresh `context.WithTimeout(context.Background(), promptAuditWriteTimeout)` for the database call. Log `prompt_audit.persist_failed` on error without exposing `PromptText`.

Register `NewPromptAuditService` in `backend/internal/service/wire.go`.

**Step 6: Run service and repository tests**

Run:

```bash
cd backend
go test ./internal/service -run TestPromptAudit -count=1
go test ./internal/repository -run TestPromptAuditRepository -count=1
```

Expected: PASS.

**Step 7: Commit**

```bash
git add backend/internal/service/prompt_audit.go backend/internal/service/prompt_audit_test.go backend/internal/repository/prompt_audit_repo.go backend/internal/repository/prompt_audit_repo_test.go backend/internal/service/wire.go backend/internal/repository/wire.go
git commit -m "feat(omni): persist prompts through fail-open audit queue"
```

### Task 4: Connect audit recording to authenticated request handlers

**Files:**
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/content_moderation_helper.go`
- Modify: `backend/internal/handler/openai_gateway_handler_test.go`
- Modify: `backend/internal/handler/content_moderation_helper_test.go` or create it if absent
- Modify generated Wire output under `backend/cmd/server/` via `go generate ./cmd/server`

**Step 1: Write failing handler tests**

Add a recording fake `PromptAuditRepository`, construct a real `PromptAuditService`, and prove:

- an authenticated OpenAI Responses request records its latest user Prompt;
- an authenticated Chat Completions request records its latest user Prompt;
- an OpenAI Responses WebSocket `response.create` frame records its latest user Prompt;
- a tool-only continuation does not record again;
- a failing audit repository does not alter the handler's existing HTTP/WebSocket response.

Assert metadata contains the authenticated `user_id`, `api_key_id`, optional `group_id`, inbound endpoint, protocol, requested model, and request ID.

**Step 2: Run the focused handler tests and verify they fail**

Run:

```bash
cd backend
go test ./internal/handler -run 'Test.*PromptAudit' -count=1
```

Expected: FAIL because handlers do not own or invoke `PromptAuditService`.

**Step 3: Inject the service into the two gateway handlers**

Add:

```go
promptAuditService *service.PromptAuditService
```

to both `GatewayHandler` and `OpenAIGatewayHandler`, and add the constructor parameter next to `contentModerationService`.

In `content_moderation_helper.go`, keep existing call sites stable by changing both `checkContentModeration` methods to:

1. build and submit the prompt audit record first when the prompt service exists;
2. then run the existing content-moderation behavior unchanged;
3. return the same moderation decision as before.

Add a dedicated builder:

```go
func buildPromptAuditInput(
    c *gin.Context,
    apiKey *service.APIKey,
    subject middleware.AuthSubject,
    protocol string,
    model string,
    body []byte,
) service.PromptAuditInput
```

Use the same authenticated metadata sources already used by `buildContentModerationInput`. Do not log Prompt text.

This seam already covers HTTP Responses, Chat Completions, Anthropic Messages, Gemini, image prompts, and each WebSocket `response.create` frame. Existing tool-continuation calls are safely skipped by the extractor.

**Step 4: Regenerate Wire output**

Run:

```bash
cd backend
go generate ./cmd/server
```

Expected: generated dependency injection compiles with `PromptAuditRepository`, `PromptAuditService`, `GatewayHandler`, and `OpenAIGatewayHandler` wired exactly once.

**Step 5: Run focused handler and compilation tests**

Run:

```bash
cd backend
go test ./internal/handler -run 'Test.*PromptAudit|TestOpenAIResponsesWebSocket_ContentModerationBlocksFirstFrame' -count=1
go test ./cmd/server -count=1
```

Expected: PASS; existing content moderation behavior remains unchanged.

**Step 6: Commit**

```bash
git add backend/internal/handler/gateway_handler.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/content_moderation_helper.go backend/internal/handler/*prompt*audit* backend/internal/handler/openai_gateway_handler_test.go backend/cmd/server
git commit -m "feat(omni): record authenticated request prompts"
```

Before committing, inspect `git diff --cached --name-only` so unrelated handler tests or generated files are not staged.

### Task 5: Verify the complete Omni-only change

**Files:**
- Modify only if a test reveals a defect in the files already listed above.

**Step 1: Format all changed Go files**

Run:

```bash
cd backend
gofmt -w internal/service/prompt_audit*.go internal/repository/prompt_audit*.go internal/handler/content_moderation_helper*.go internal/handler/gateway_handler.go internal/handler/openai_gateway_handler.go
```

Expected: no formatting diff remains after a second `gofmt` run.

**Step 2: Run focused packages**

Run:

```bash
cd backend
go test ./internal/service ./internal/repository ./internal/handler -count=1
```

Expected: PASS.

**Step 3: Run the backend regression suite**

Run:

```bash
cd backend
go test ./... -count=1
```

Expected: PASS, or document an independently reproduced pre-existing failure without weakening the new focused tests.

**Step 4: Run static checks**

Run:

```bash
cd backend
go vet ./...
```

Expected: PASS.

**Step 5: Inspect branch and scope**

Run:

```bash
git branch --show-current
git status --short --branch
git diff origin/dev/omni...HEAD --name-only
```

Expected:

- branch is exactly `dev/omni`;
- only Prompt-audit design/plan, migration, service, repository, handler, tests, and generated Wire files changed;
- existing user-owned `.gitignore` and `AGENTS.md` changes remain outside feature commits;
- no commits or file changes were made to `main`, `dev/team`, or company branches.

**Step 6: Add a final fixup commit only if verification required changes**

```bash
git add <only-files-changed-for-verification>
git commit -m "test(omni): verify prompt audit persistence"
```

Skip this commit when verification makes no changes.
