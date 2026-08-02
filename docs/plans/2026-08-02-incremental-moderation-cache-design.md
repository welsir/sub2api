# Incremental Full-Context Moderation Cache Design

## Goal

Keep the OAI account boundary fail-closed while avoiding repeated MiniMax calls
for conversation history that Sub2API has already reviewed. Every visible text
character remains covered by semantic moderation; only exact, policy-versioned
cache hits may skip a provider call.

The latency target is approximately 1-1.5 seconds for a normal appended turn.
A cold, previously unseen very large transcript may take up to the configured
five-second moderation deadline. Missing, expired, corrupt, or unavailable cache
state never permits an OAI request.

## Decision

Implement deterministic overlapping chunks and a Redis decision cache inside
Sub2API, before the existing OpenAI-shaped `POST /v1/moderations` adapter call.
This keeps the provider adapter stateless and reuses Sub2API's existing Redis
connection, request scoping, retry rules, logs, and pre-account-selection
boundary.

Rejected alternatives:

1. MiniMax automatic prompt caching still requires uploading the full request
   body and did not materially reduce the measured 160,000-character latency.
2. An in-memory adapter cache is lost on restart, cannot be shared across
   replicas, and makes rollback/warm-up behavior unpredictable.
3. Reviewing only the last user message permits dangerous history followed by
   `Continue` to reach OAI and therefore does not meet the account-safety goal.

## Chunking And Coverage

- Normalize the complete protocol-visible transcript exactly once using the
  existing extraction path.
- Split text into deterministic 32,768-character windows with a 1,024-character
  overlap. Boundaries are measured in Unicode code points, not bytes.
- Every character must occur in at least one chunk. Empty text produces no text
  chunk; structured images retain the existing deterministic local block.
- Include a fixed chunking/policy revision in every cache key. Changing chunk
  size, overlap, classifier policy, model, endpoint, or relevant thresholds must
  produce a different namespace rather than reuse stale decisions.
- Exact appended histories keep all completed prefix chunks stable. Normally
  only the previous partial tail and the newly appended tail require review.

The overlap protects intent near a mechanical boundary. This first release does
not create an LLM-generated conversation summary because a summary would become
another lossy safety boundary.

## Cache Contract

Store only a compact verdict keyed by SHA-256 of the versioned normalized chunk.
The cache never stores prompt text.

- `allow`: short-lived safe verdict, initially 24 hours.
- `block`: longer-lived unsafe verdict, initially 30 days.
- unknown value, missing key, Redis error, or expired entry: cache miss.

Redis reads may be batched, but cache writes happen only after a strict parsed
provider result. Provider errors, timeouts, cancellations, malformed responses,
and local overload must not create an allow entry.

## Request Flow

1. Apply the existing enabled/mode/group/model and keyword scope.
2. Extract the complete transcript, images, and deterministic overlapping text
   chunks.
3. Build the policy namespace and batch-read cached verdicts.
4. Immediately block if any cached chunk is unsafe.
5. Review cache misses with bounded parallelism through the existing moderation
   adapter.
6. Immediately block if any reviewed chunk is unsafe; cancel remaining work
   where possible.
7. Cache strict allow/block results with their respective TTLs.
8. Allow account selection only when every chunk is a strict allow and all cache
   operations required for that decision have succeeded.

The request retains one overall moderation deadline. It does not receive three
complete-transcript retries. Individual transient provider failures use the
existing bounded retry policy, but malformed classifier output is converted to
a local unsafe result by the adapter so it cannot amplify into repeated long
requests.

## Failure And Rollback Behavior

- Redis unavailable: return the configured local moderation error; do not call
  OAI.
- MiniMax unavailable, timed out, malformed, or rate-limited after the retry
  budget: return the local moderation error; do not call OAI.
- Transcript too large for one provider request: split it; size alone is not an
  allow condition.
- Text beyond the configured total local safety bound: return a local error; do
  not truncate and do not call OAI.
- Images remain locally blocked until a real image-capable moderation backend is
  integrated.

The deployment remains a versioned image switch. The existing production image,
Compose backup, moderation configuration backup, and prior adapter container
remain rollback targets. Database and Redis containers are not restarted.

## Observability

Record aggregate metadata only:

- total chunks, cache hits, cache misses, reviewed chunks;
- allow/block/error outcome and total moderation latency;
- policy namespace revision and provider latency;
- request ID, user/key IDs already present in the moderation log.

Do not log cache keys, full chunk hashes together with prompt excerpts, raw
provider bodies, credentials, or complete transcripts.

## Verification

Automated tests must prove:

- all code points are covered and adjacent chunks overlap exactly;
- an appended transcript reuses stable prefix chunks and reviews only its tail;
- `Continue` after dangerous history blocks even when the final message is safe;
- cached unsafe chunks block without calling MiniMax;
- safe results are reused only within the same policy namespace;
- Redis read/write errors, unknown cache values, provider failures, and malformed
  output fail closed;
- images fail closed without provider or OAI calls;
- no blocked or failed request creates a `usage_logs` row or downstream
  `account_id`.

Offline replay must run with OAI egress disabled. Production rollout begins with
an isolated adapter/config target, then a versioned application image, one
high-risk canary, one appended-history canary, and a bounded observation window.

## Non-Goals

- Automatically banning a user or API key.
- Sampling in-scope pre-block requests.
- Persisting prompt text in Redis.
- Relying on MiniMax prompt caching as the safety cache.
- Synchronizing this Omni change to `main`, company, or team branches.
