# Complete Upstream Audit Design

## Goal

Record the final bytes sent to an upstream model provider and the complete application-body bytes consumed from every audited upstream attempt on `dev/omni`, without allowing audit failures to block or destabilize model traffic. Archive the previous Shanghai calendar day to the Mac at 09:00, verify it, then reclaim the corresponding PostgreSQL partition physically.

## Confirmed Runtime Facts

- Production is PostgreSQL 18.4 with `default_toast_compression = pglz`.
- The legacy `prompt_audit_logs` table stored extracted text only. At inspection time it used 45 MB for 1,179 rows; `prompt_text` averaged 22,620 bytes and peaked at 68,088 bytes.
- The 2026-07-20 local archive contains 3,903 legacy rows and is 10,752,776 bytes after gzip compression.
- The backend already depends on `github.com/klauspost/compress/zstd`.

## Chosen Architecture

### Capture boundary

The inbound handler attaches audit identity metadata to the request context but does not persist the inbound body. The shared `HTTPUpstream` implementation captures the body on the actual `http.Request` immediately before `client.Do`, after model mapping, system/tool injection, protocol conversion, retry mutation, and account selection. Each `Do` or `DoWithTLS` call receives its own atomic attempt number.

OpenAI Responses WebSocket traffic bypasses `HTTPUpstream`, so its final `response.create` payload and every raw upstream WebSocket message are captured explicitly around `WriteJSONWithContextTimeout` and `ReadMessageWithContextTimeout`.

### Audit format

Each upstream attempt is one append-only row containing:

- identity: request id, user id, API key id, group id;
- route: inbound endpoint/protocol/model and upstream account id, transport, method, sanitized URL host/path, attempt number;
- request: content type, SHA256, raw byte count, compressed byte count, zstd-compressed exact body;
- response: status code, sanitized response-header JSON, SHA256, application-body byte count, compressed byte count, zstd-compressed exact body/event stream;
- outcome: `completed`, `transport_error`, `read_error`, `closed`, `capture_overflow`, `upstream_error`, or `audit_error`, plus a bounded and credential-sanitized error string and timestamps.

For HTTP, the contract is the exact body byte stream consumed by gateway code after HTTP content decoding, not TLS frames or transfer/content-encoding wire bytes. SSE is not converted into a reconstructed assistant answer: those application bytes are retained in read order. WebSocket output uses a JSONL envelope where each line is a JSON string containing one exact raw message; this preserves whitespace, newlines, and message boundaries without base64.

Authorization, Proxy-Authorization, Cookie, Set-Cookie, API-key headers, signed query credentials, and account tokens are never stored. URL metadata is limited to scheme, host, and path. The transmitted request body is not heuristically redacted because that would prevent exact reconstruction; archives therefore remain sensitive files with mode `0600` under a `0700` directory.

### Failure and load protection

Audit is deliberately fail-open. Compression, queue, database, missing-partition, oversized-capture, or response-finalization failures only emit bounded structured warnings and counters; they never alter the upstream result.

Capture has both a per-attempt raw-byte ceiling and global active/queued byte budgets. Request goroutines only copy/hash bounded bytes; zstd compression and PostgreSQL writes run in background workers. When any boundary is exceeded, the attempt is recorded if possible with `capture_overflow` and hashes/sizes where available, but the complete body may be absent. This is the accepted exception to complete capture and prevents large images or stalled streams from exhausting the 43 host.

Rows are enqueued only after transport failure or response completion/close. A process crash can lose in-flight or queued audit data; this is accepted by the selected asynchronous fail-open policy.

## PostgreSQL Layout and Physical Retention

`upstream_audit_logs` is range-partitioned by `created_at` using Asia/Shanghai day boundaries. Payloads are application-zstd-compressed into `BYTEA`; JSONB is not used for request/response bodies because it cannot preserve exact bytes, key order, non-JSON payloads, or SSE framing.

A database function creates a named daily partition under an advisory lock and refuses to recreate an already-dropped closed-day partition. The repository ensures the target partition before inserting each row. Indexes live on the partitioned parent for request-id, user/time, API-key/time, and outcome/time lookups.

At 09:00 the archive script targets yesterday's closed partition, streams it as a deterministic CSV gzip file, verifies header, row count, zstd decoding, raw sizes, and hashes, then writes and fsyncs the archive, analysis, summary, and manifest. Deletion uses one PostgreSQL transaction that takes an `ACCESS EXCLUSIVE` lock, rechecks the archived row count and maximum ID, and drops that exact daily partition. The lock removes the late-insert race between verification and deletion. Dropping a daily partition releases relation and TOAST files immediately, unlike ordinary `DELETE`, which leaves reusable dead space until vacuum and does not normally shrink the underlying files.

If the partition is missing, still changing, contains incomplete rows that policy says to retain, has a count/checksum mismatch, or the local archive cannot be fsynced and re-read, no online deletion occurs.

## Archive Analysis

After successful verification and online cleanup, the local job decompresses request and response bodies in memory one row at a time and writes `analysis.json` plus a concise `summary.md`. Analysis includes counts by protocol/model/status/outcome, success/error rates, input/output byte totals, compression ratios, and bounded content-category summaries. It must not echo credentials or dump full prompts/responses into the automation report.

## Verification

- RED/GREEN service tests for final request bytes, retry attempts, transport errors, successful responses, error responses, raw SSE capture, close-before-EOF, overflow, queue-full fail-open, zstd round-trip, hashes, and header/URL sanitization.
- Repository tests for partition ensure and all persisted fields.
- Migration tests for partitioning, columns, indexes, and helper function.
- Python tests for deterministic export, manifest/checksum gates, exact partition-drop command, resume safety, decompression, and content analysis.
- Targeted Go tests, migration integration checks where local PostgreSQL is available, Python tests, formatting, and final diff/status audit.
