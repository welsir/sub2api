## ADDED Requirements

### Requirement: Moderations API compatibility
The adapter SHALL expose a Bearer-authenticated `POST /v1/moderations` endpoint
that accepts the model and text-input shape used by Sub2API, selects a configured
Qwen or MiniMax backend, and returns an OpenAI-shaped response containing
`classifier_policy_revision`, `results[].flagged`, `results[].categories`, and
`results[].category_scores`.

#### Scenario: Sub2API-compatible unsafe response
- **WHEN** an authenticated text request produces a valid Qwen3Guard `Unsafe` classification
- **THEN** the adapter returns a successful Moderations response whose evaluated category score is sufficient for Sub2API to flag the request

#### Scenario: Missing or invalid authentication
- **WHEN** a request omits the configured Bearer secret or supplies the wrong value
- **THEN** the adapter rejects the request without invoking the model backend or disclosing secret details

#### Scenario: MiniMax strict allow response
- **WHEN** MiniMax returns a complete strict final JSON decision of `allow` without provider-sensitive flags
- **THEN** the adapter returns a successful Moderations response with zero evaluated policy scores

#### Scenario: Trusted policy and untrusted transcript remain separate
- **WHEN** the MiniMax backend request is built from a Sub2API transcript
- **THEN** the adapter sends exactly one trusted system classifier instruction and one user message containing the transcript verbatim as untrusted data

#### Scenario: Transcript tries to override classification
- **WHEN** visible text asks MiniMax to ignore moderation, replace system instructions, return allow, or discounts risk as sandbox, owned-app, authorization, or internal testing
- **THEN** the trusted classifier policy ignores the override claim and still evaluates any visible actionable abuse, authorization bypass, malware, evasion, or dangerous executable execution

#### Scenario: Canonical attachment marker is present
- **WHEN** the transcript contains text matching the canonical controlled marker grammar with bounded kind, normalized MIME, source class, and normalized extension
- **THEN** the marker is neutral uninspected metadata, all surrounding text remains untrusted semantic input, and adding the marker cannot turn visible high-risk text into allow

#### Scenario: MiniMax block, review, or sensitive response
- **WHEN** MiniMax returns `block`, `review`, `input_sensitive`, `output_sensitive`, provider code `1026`, or provider code `1027`
- **THEN** the adapter returns a successful flagged Moderations response and never treats the provider refusal as safe

#### Scenario: MiniMax deterministic account failure
- **WHEN** MiniMax reports invalid authentication, insufficient balance, plan usage exhaustion `2056`, or no active plan `2062`
- **THEN** the adapter returns a deterministic 401 or 402 so Sub2API terminates without consuming transient retry attempts

#### Scenario: MiniMax uncertain classifier output
- **WHEN** MiniMax returns no choice, a non-stop finish, empty content, or an invalid final decision after a successful HTTP response
- **THEN** the adapter returns a successful flagged Moderations response so uncertainty blocks locally without retrying the transcript

#### Scenario: MiniMax transient transport or provider failure
- **WHEN** MiniMax times out, throttles, returns a transient provider error, or returns an invalid HTTP response body
- **THEN** the adapter returns a visible retryable non-2xx result and never synthesizes an allow response

#### Scenario: MiniMax M2.7 high-speed request
- **WHEN** the configured MiniMax model is `MiniMax-M2.7-highspeed`
- **THEN** the adapter selects the accelerated lane through the model name, omits `service_tier`, bounds completion tokens, and retains the same strict final-decision parser

#### Scenario: MiniMax M3 deterministic request
- **WHEN** the configured MiniMax model is `MiniMax-M3`
- **THEN** the adapter uses temperature zero, disables thinking, bounds completion tokens, and retains the same strict final-decision parser

#### Scenario: Direct structured image bypasses the normal projection
- **WHEN** a caller sends a structured image directly to the adapter instead of the normal Sub2API text projection
- **THEN** the adapter returns a deterministic successful flagged local-policy result without calling MiniMax; operators record this as defense in depth, not image-pixel moderation or the normal Sub2API attachment path

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

### Requirement: Classifier uncertainty fails closed
The adapter MUST NOT convert malformed, empty, truncated, or unknown classifier output into a safe moderation result. Qwen parse failures remain visible errors; MiniMax classifier-shape uncertainty becomes a successful blocked result to avoid retrying a large transcript.

#### Scenario: Malformed model output
- **WHEN** the Qwen backend returns output that does not match the supported structured classification format
- **THEN** the adapter returns a non-2xx error, records a redacted parse-error event, and leaves the caller's configured failure policy to decide request handling

#### Scenario: Malformed MiniMax final decision
- **WHEN** MiniMax completes the HTTP request but its classifier output does not match the strict final-decision format
- **THEN** the adapter returns a flagged illicit result and does not expose a retryable parse failure

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
The adapter SHALL expose separate liveness and readiness checks so operators can distinguish a running process from a loaded and callable model backend. Readiness and every successful Moderations response SHALL expose the active classifier policy revision for Sub2API's cache-compatibility handshake.

#### Scenario: Process alive but model unavailable
- **WHEN** the adapter process is running but the model backend is loading or unreachable
- **THEN** `/healthz` reports liveness while `/readyz` reports not ready

#### Scenario: MiniMax model list succeeds but inference quota is unavailable
- **WHEN** `/v1/models` is reachable and contains the configured model but no successful classification has been completed with the candidate credential
- **THEN** operators treat readiness as connectivity and model-presence evidence only and keep the application release gate closed until a real classification succeeds

#### Scenario: Caller expects another classifier policy
- **WHEN** Sub2API's configured classifier policy revision does not equal the revision exposed by `/readyz` or a successful Moderations response
- **THEN** Sub2API treats the adapter result as incompatible: a readiness mismatch prevents cache reads and writes for that request, while a response mismatch prevents writing that verdict and invalidates the remembered readiness check

### Requirement: Redacted operational evidence
The adapter MUST record model revision, mapping revision, request correlation, input size or hash, classification, latency, and error class without logging full prompts, Bearer secrets, or model-server credentials.

#### Scenario: Successful moderation log
- **WHEN** an authenticated moderation request completes
- **THEN** the operational record contains enough metadata to correlate and reproduce the classifier version without containing the original prompt
