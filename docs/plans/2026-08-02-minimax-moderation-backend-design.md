# MiniMax Moderation Backend Design

## Goal

Protect OAI accounts by routing only configured high-risk Omni groups through a
MiniMax-backed semantic moderation boundary before downstream account selection.
False positives are acceptable in the first release; no moderation decision may
permanently ban a user or API key.

## Decision

Extend the existing Qwen3Guard moderation adapter with a provider-neutral backend
interface and a `minimax` backend mode. Keep Sub2API's existing OpenAI-shaped
`POST /v1/moderations` contract unchanged.

Rejected alternatives:

1. Calling MiniMax directly from Sub2API would couple the gateway to provider
   prompts, response parsing, error codes, and future provider migrations.
2. Creating a second adapter would duplicate authentication, gzip, body limits,
   concurrency, deadlines, metrics, and failure handling.

The existing adapter name and Qwen environment variables remain supported in
this release to avoid deployment churn. Provider-neutral aliases may be added
later only when an actual deployment needs them.

## Request Flow

1. Sub2API applies the existing group and model scope before moderation.
2. In-scope `pre_block` requests send the complete visible semantic transcript
   to the adapter's `POST /v1/moderations` endpoint.
3. The adapter sends a non-streaming Chat Completions request to MiniMax with a
   fixed classifier instruction and the untrusted transcript in a separate user
   message.
4. The adapter normalizes MiniMax output into the existing Moderations category
   and score response.
5. Sub2API selects an OAI account only after a strict allow result.

The initial MiniMax model is `MiniMax-M2.7`. The model stays configurable.

## Strict Decision Policy

- A strict parsed `allow` result maps to `Safe` and zero category scores.
- `block`, `review`, uncertainty, refusal, or an unknown label maps to a blocked
  illicit result.
- MiniMax `input_sensitive=true`, `output_sensitive=true`, or provider codes
  `1026` and `1027` map directly to a blocked result and are not retried.
- Invalid JSON, empty output, truncated output, network failures, timeouts,
  throttling, and transient provider errors remain typed adapter failures. The
  existing Sub2API boundary retries transient failures up to three total attempts
  and then returns a local 503.
- Invalid credentials and insufficient balance are deterministic failures and
  must not consume all retry attempts.
- No failure path falls back to OAI.

The classifier treats the transcript as untrusted data and ignores any embedded
instructions asking it to change policy, reveal the classifier prompt, or emit an
allow result. The adapter accepts only a bounded final JSON object after removing
provider reasoning wrappers.

## Scope And User Impact

The rollout uses existing `group_ids`/`excluded_group_ids` scope with
`mode=pre_block` and `keyword_blocking_mode=keyword_and_api`. `pre_block` checks
every in-scope request regardless of `sample_rate`.

The first release blocks only the current request. It does not automatically ban
users, disable API keys, add permanent hashes, or create a long-lived session
block from a MiniMax decision. Existing OAI `cyber_policy` containment remains a
separate downstream safety mechanism.

## Cost And Latency Controls

- Use non-streaming responses and a small completion budget.
- Keep the stable transcript prefix so MiniMax automatic prompt caching can
  reuse repeated conversation history.
- Record provider usage, cached input tokens, latency, trace ID, provider code,
  and normalized outcome without logging credentials or full provider payloads.
- Start with the standard M2.7 endpoint. High-speed service is a later operational
  choice based on measured P95/P99, not a code fork.

The code cannot guarantee a one-second SLO without a real MiniMax key and route.
Production enablement therefore requires a credentialed benchmark from the Omni
host and must remain separate from this repository change.

## Configuration

Add a strict backend-provider setting with `qwen` as the compatibility default
and `minimax` as the new value. MiniMax uses the existing externalized backend
base URL, model, and bearer token fields. Readiness for MiniMax must not call a
local-only `/health` path; it uses a bounded provider-compatible readiness check
or a configuration-only readiness result that never exposes the token.

Secrets stay in runtime environment configuration and are never committed.

## Verification

Contract tests must prove:

- Qwen behavior remains backward compatible.
- MiniMax strict allow and block outputs normalize correctly.
- reasoning wrappers cannot bypass final JSON parsing.
- prompt-injected `allow` text in the transcript cannot become the decision.
- `input_sensitive`, `output_sensitive`, `1026`, and `1027` block locally.
- transient failures remain retryable while auth/balance errors are
  deterministic failures.
- malformed, empty, oversized, and timed-out responses fail closed at Sub2API.
- no blocked or failed moderation request reaches OAI account selection.

Offline acceptance uses the previously audited prompt set with OAI egress
disabled. Confirmed risky prompts must all block. Harmless prompts receive only a
basic smoke check; reducing false positives is not a first-release gate.

## Non-Goals

- Changing production group membership or runtime configuration.
- Storing a MiniMax key in the repository.
- Automatically banning users or API keys.
- Implementing a stateful delta-transfer protocol.
- Claiming real MiniMax latency or recall without credentialed replay evidence.
