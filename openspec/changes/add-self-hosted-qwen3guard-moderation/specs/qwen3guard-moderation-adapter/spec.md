## ADDED Requirements

### Requirement: Moderations API compatibility
The adapter SHALL expose a Bearer-authenticated `POST /v1/moderations` endpoint that accepts the model and text-input shape used by Sub2API, selects a configured Qwen or MiniMax backend, and returns an OpenAI-shaped response containing `results[].flagged`, `results[].categories`, and `results[].category_scores`.

#### Scenario: Sub2API-compatible unsafe response
- **WHEN** an authenticated text request produces a valid Qwen3Guard `Unsafe` classification
- **THEN** the adapter returns a successful Moderations response whose evaluated category score is sufficient for Sub2API to flag the request

#### Scenario: Missing or invalid authentication
- **WHEN** a request omits the configured Bearer secret or supplies the wrong value
- **THEN** the adapter rejects the request without invoking the model backend or disclosing secret details

#### Scenario: MiniMax strict allow response
- **WHEN** MiniMax returns a complete strict final JSON decision of `allow` without provider-sensitive flags
- **THEN** the adapter returns a successful Moderations response with zero evaluated policy scores

#### Scenario: MiniMax block, review, or sensitive response
- **WHEN** MiniMax returns `block`, `review`, `input_sensitive`, `output_sensitive`, provider code `1026`, or provider code `1027`
- **THEN** the adapter returns a successful flagged Moderations response and never treats the provider refusal as safe

#### Scenario: MiniMax deterministic account failure
- **WHEN** MiniMax reports invalid authentication or insufficient balance
- **THEN** the adapter returns a deterministic 401 or 402 so Sub2API terminates without consuming transient retry attempts

#### Scenario: MiniMax transient or malformed response
- **WHEN** MiniMax times out, throttles, returns a transient provider error, or emits an invalid final decision
- **THEN** the adapter returns a visible retryable non-2xx result and never synthesizes an allow response

#### Scenario: MiniMax M3 low-latency request
- **WHEN** the configured MiniMax model is `MiniMax-M3` and the service tier is `priority`
- **THEN** the adapter disables thinking, requests priority admission, bounds completion tokens, and retains the same strict final-decision parser

#### Scenario: Unsupported multimodal input
- **WHEN** a Moderations request contains an image or another unsupported non-text part
- **THEN** the adapter returns a clear non-2xx error and does not silently classify the input as safe

### Requirement: Deterministic classification mapping
The adapter MUST translate Qwen3Guard safety labels and categories through a versioned deterministic mapping into Sub2API-evaluated moderation categories. `Safe`, `Controversial`, and `Unsafe` MUST use documented policy values rather than being described as calibrated probabilities.

#### Scenario: Safe classification
- **WHEN** Qwen3Guard returns `Safe`
- **THEN** the adapter returns `flagged=false` and zero policy scores for evaluated categories

#### Scenario: Controversial classification
- **WHEN** Qwen3Guard returns `Controversial`
- **THEN** the adapter returns `flagged=false`, records the raw label/category operationally, and emits the documented intermediate policy value

#### Scenario: Unsafe category without a direct mapping
- **WHEN** Qwen3Guard returns `Unsafe` with an absent, unknown, or non-equivalent category
- **THEN** the adapter applies the documented unsafe fallback to a Sub2API-evaluated category so the taxonomy mismatch cannot produce an allowed score set

### Requirement: Parse failures fail visibly
The adapter MUST NOT convert malformed, empty, truncated, or unknown Qwen3Guard output into a safe moderation result.

#### Scenario: Malformed model output
- **WHEN** the model backend returns output that does not match the supported structured classification format
- **THEN** the adapter returns a non-2xx error, records a redacted parse-error event, and leaves the caller's configured failure policy to decide request handling

### Requirement: Bounded runtime behavior
The adapter SHALL enforce configured compressed-body, decompressed-body, input, concurrency, queue, and inference-time limits and SHALL return explicit overload or timeout errors instead of allowing unbounded work accumulation.

#### Scenario: Gzip full-context request
- **WHEN** Sub2API sends a valid gzip-encoded Moderations JSON request within both body limits
- **THEN** the adapter decompresses it, validates the same authenticated contract, and invokes the backend with the complete text

#### Scenario: Invalid or oversized compressed request
- **WHEN** a gzip body is malformed or expands beyond the configured body limit
- **THEN** the adapter returns a clear non-2xx error without invoking the model backend

#### Scenario: Concurrency limit reached
- **WHEN** all inference slots and the bounded queue are occupied
- **THEN** the adapter rejects excess work with a retryable service error and records an overload metric

#### Scenario: Inference exceeds its deadline
- **WHEN** Qwen3Guard does not complete within the configured inference deadline
- **THEN** the adapter cancels or abandons the request, returns a timeout-class error, and does not retry indefinitely

### Requirement: Health and model readiness
The adapter SHALL expose separate liveness and readiness checks so operators can distinguish a running process from a loaded and callable model backend.

#### Scenario: Process alive but model unavailable
- **WHEN** the adapter process is running but the model backend is loading or unreachable
- **THEN** `/healthz` reports liveness while `/readyz` reports not ready

### Requirement: Redacted operational evidence
The adapter MUST record model revision, mapping revision, request correlation, input size or hash, classification, latency, and error class without logging full prompts, Bearer secrets, or model-server credentials.

#### Scenario: Successful moderation log
- **WHEN** an authenticated moderation request completes
- **THEN** the operational record contains enough metadata to correlate and reproduce the classifier version without containing the original prompt
