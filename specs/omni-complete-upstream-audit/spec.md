# Omni Complete Upstream Audit Specification

## Required behavior

- Record the exact final request bytes for each real upstream attempt, not merely the inbound prompt.
- Record successful, failed, and streaming upstream application-body output with explicit completeness/outcome metadata.
- Treat retries as independent attempts correlated by inbound request id and attempt number.
- Use asynchronous fail-open behavior; audit failures must not block model traffic.
- Never persist authorization, cookies, API keys, account tokens, or signed query credentials in metadata.
- Keep online data for a short period and archive the previous Shanghai day at 09:00.
- Delete online data only after local row-count, integrity, fsync, and SHA256 checks succeed.
- Reclaim PostgreSQL storage physically through closed daily partitions rather than ordinary row deletion.
- Produce a post-archive analysis summary without dumping complete sensitive content into automation output.
- Do not deploy to production without separate user confirmation.

## Acceptance

The Go and Python test suites prove exact byte round-trips, fail-open behavior, raw stream framing, failed response capture, partition-safe archive/drop, and analysis generation. The final diff is limited to `dev/omni` and the authorized local archive script/automation.
