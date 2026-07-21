# Omni Complete Upstream Audit Run Log

## Scope

- Branch: `dev/omni`
- Production deployment: explicitly excluded pending separate user confirmation
- External artifacts: local archive script and Codex automation `omni-prompt`

## Runtime evidence

- Read-only production query on 2026-07-21 reported PostgreSQL 18.4 and `default_toast_compression=pglz`.
- Legacy `prompt_audit_logs`: 45 MB, 1,179 live rows, average `pg_column_size(prompt_text)=22,620`, maximum 68,088 bytes.
- Local 2026-07-20 archive: 3,903 rows, 35,237,824-byte CSV, 10,752,776-byte gzip.

## Storage estimate

The legacy archive is a lower bound because it contains only extracted prompt text. At 3,903 attempts/day it averaged about 9.0 KB of uncompressed CSV and 2.75 KB of gzip per row. Complete final request payloads include instructions, tools, conversation state, and protocol wrappers; complete responses add another body of comparable but workload-dependent size.

Planning range for the observed traffic:

- raw complete request and response bytes: roughly 70-350 MB/day for ordinary text/tool traffic;
- application-zstd payloads plus row/index overhead: roughly 20-140 MB/day;
- online peak before the 09:00 archive drops yesterday: roughly 1.4 daily volumes, or 30-200 MB in normal conditions;
- large image/file bodies may exceed this range, but the 16 MiB per-attempt capture ceiling converts them to `capture_overflow` instead of risking gateway memory exhaustion.

The first production day should measure `SUM(request_compressed_bytes + response_compressed_bytes)`, partition relation size, queue-drop warnings, and overflow counts. Adjust the capture ceiling only from those observations.

## Privacy boundary

- Persist exact transmitted body bytes so requests remain reconstructable.
- Do not persist Authorization, Proxy-Authorization, Cookie, Set-Cookie, API-key headers, account tokens, or URL query credentials.
- Store only scheme/host/path for the upstream URL.
- Keep archive directory `0700` and files `0600`; content analysis outputs aggregate counts only.

## Failure behavior

- Bounded asynchronous queue; request goroutines only copy/hash bounded bytes, while zstd and PostgreSQL writes run in two background workers. Queue/database/compression errors log and drop audit data without changing model results.
- Limits are 16 MiB per request or response body, 64 MiB globally across active captures, 64 MiB queued raw payloads, and 256 queued attempts.
- Transport errors, upstream failures, early closes, read errors, and successful EOF/terminal events get distinct outcomes when capture succeeds.
- In-flight and queued records may be lost on process crash by deliberate fail-open policy.
- Archive mismatch, zstd/hash failure, changing snapshot, missing partition, artifact checksum mismatch, or local fsync failure prevents online partition deletion.
- The final delete is a single transaction: acquire the same advisory lock as partition creation, lock the exact child table, recheck row count/max ID, then DROP. A closed partition cannot be recreated after it has been removed.

## Verification evidence

- `go test ./... -count=1` from `backend/`: PASS across the backend.
- `go test -race ./internal/service ./internal/repository -run 'Test(HTTPUpstreamAudit|WebSocketUpstreamAudit|UpstreamAudit|PromptAudit|HTTPUpstreamSuite)' -count=1 -v`: PASS, including queue/database fail-open, raw byte budgets, transport capture, URL/token redaction, retry numbering, SSE/WS framing, zstd round-trip, and real HTTP transport boundary.
- `go vet ./internal/service ./internal/repository ./internal/handler ./migrations`: PASS.
- `python3 -m py_compile export_previous_day.py test_export_previous_day.py`: PASS.
- `python3 -m unittest -v test_export_previous_day.py`: 7/7 PASS, covering zstd/size/hash verification, large fields, partition validation, lock-and-snapshot DROP SQL, closed-day guard, artifact checksum verification, and aggregate analysis.
- `go test -tags=integration ./internal/repository -run 'Test(MigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|UpstreamAuditPartitionFunctionUsesShanghaiBoundaryAndIsConcurrentSafe)' -count=1 -v`: command PASS but PostgreSQL integration cases were skipped because local Docker was unavailable. No production database was mutated to compensate.
- `git diff --check`: PASS.

## Review findings closed locally

- Refreshed stale handler contexts in both `/v1/responses` HTTP and WS ingress paths.
- Added capture for WS ingress multi-turn sends and `generate=false` prewarm attempts.
- Moved zstd compression out of the request/response goroutine and added a race-tested active/queue byte budget.
- Sanitized query credentials and common key/token patterns from persisted transport errors.
- Defined HTTP response bytes precisely as the application-body stream consumed after HTTP decoding; WebSocket frames use one JSON string per JSONL line.
- Removed the archive verification-to-DROP race with lock/recheck/drop in one PostgreSQL transaction, added closed-date and closed-partition recreation guards, fsynced directories, verified every manifest artifact again, and added safe recovery for a crash after DROP but before manifest update.

## Remaining risks and accepted limits

- The 16 MiB body ceiling, 64 MiB active/queued budgets, queue saturation, process crash, and repository timeout can intentionally produce a missing or incomplete audit under the accepted fail-open policy.
- A non-replayable outbound HTTP request body is not pre-consumed by auditing; that attempt is marked `audit_error` without request bytes so audit cannot change upstream read semantics.
- HTTP response bytes are the exact post-content-decoding application stream consumed by the gateway, not TLS/HTTP wire frames. WebSocket rows preserve exact message text and boundaries through JSON-string-per-line framing.
- Full request/response bodies remain sensitive even after header/query credential exclusion. PostgreSQL access and the Mac archive directory need operational access control and backup policy.
- Storage figures are planning ranges; the first production day must measure actual partition, TOAST, index, zstd, overflow, and queue-drop metrics.
- Local PostgreSQL integration coverage was skipped because Docker was unavailable. Migration 175 and the destructive archive transaction still require a PostgreSQL 18 staging/backup rehearsal before production approval.
- Handler metadata attachment, service capture, repository persistence, and real HTTP transport are covered in layers, but there is not yet one test that boots the complete handler-to-database stack and asserts the final stored row. The WS write/read loops likewise rely on capture-helper plus forwarding tests rather than a real upstream WS/database end-to-end fixture.
- Archive pure logic has seven tests, but `main()` still lacks injected SSH/PostgreSQL crash tests for every failure boundary. The first staging rehearsal must explicitly exercise snapshot change, DROP failure, and crash-after-DROP recovery.
- No live archive/export/drop dry run was performed because the task explicitly forbids production changes before review.

## Low-impact release plan

1. Back up the database and confirm free disk headroom.
2. Deploy migration 175 and the application together during a low-traffic window; do not run the new archive script before the migration is live.
3. Restart only the `sub2api` application service and verify health/models routes.
4. Send one non-stream request, one stream request, one controlled upstream failure, and one retry; verify separate rows, zstd round-trip, hashes, and unchanged client responses.
5. Observe memory, audit queue warnings, insert latency, partition size, and normal request latency for at least one traffic window.
6. Run the archive script first with `--no-delete` for the first closed day; inspect manifest, analysis, and summary.
7. After explicit user approval, run the normal mode and verify the exact yesterday partition is gone while today's partition remains.
8. The updated 09:00 automation is currently `PAUSED`; activate it only after that first manual archive acceptance and explicit release approval.

## Delivery state

- Local implementation and review fixes are complete.
- No production deployment or write was performed.
- No commit, push, or PR was created; the working tree is intentionally left for user review.
