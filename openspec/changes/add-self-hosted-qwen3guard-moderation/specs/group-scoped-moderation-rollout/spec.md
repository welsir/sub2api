## ADDED Requirements

### Requirement: Default-on scope with explicit exclusions
Sub2API SHALL support an exclusion list that applies when `all_groups=true`, SHALL moderate every group not present in that list, and SHALL preserve the existing selected-group allowlist behavior when `all_groups=false`.

#### Scenario: Existing non-exempt group
- **WHEN** an inbound text request authenticates with an API key whose group ID is not in `excluded_group_ids`
- **THEN** Sub2API applies the configured keyword and semantic moderation path before forwarding the request upstream

#### Scenario: Newly created group
- **WHEN** a new group is created after moderation configuration and its group ID is not explicitly excluded
- **THEN** requests from that group are moderated without requiring an allowlist update

#### Scenario: Request has no group ID
- **WHEN** `all_groups=true` and an authenticated request has no resolved group ID
- **THEN** Sub2API moderates the request because it does not match an explicit exemption

#### Scenario: Legacy selected-group mode
- **WHEN** `all_groups=false`
- **THEN** Sub2API retains the existing `group_ids` allowlist behavior and does not combine it ambiguously with exclusion semantics

### Requirement: MiniMax-first scope is the approved high-risk allowlist
The initial 43 V2 MiniMax rollout SHALL use `all_groups=false`, SHALL include
exactly approved group IDs `8`, `10`, `13`, `14`, and `15`, and SHALL NOT apply
hosted semantic moderation to other groups.

#### Scenario: Request uses an approved high-risk group
- **WHEN** an inbound request authenticates with an API key bound to group ID `8`, `10`, `13`, `14`, or `15`
- **THEN** Sub2API applies keyword and semantic moderation before downstream account selection

#### Scenario: Request uses another group
- **WHEN** an inbound request authenticates with an API key outside the approved high-risk group IDs
- **THEN** Sub2API does not invoke the MiniMax moderation path for that request

#### Scenario: Group display name resembles an approved group
- **WHEN** a different group has a similar display name but another stable ID
- **THEN** Sub2API applies scope by ID and does not infer membership from the name

### Requirement: Exemption configuration is visible and validated
The Sub2API admin API, configuration view, runtime status, and management UI MUST expose normalized excluded group IDs and MUST reject nonexistent exemption IDs.

#### Scenario: Operator configures a future default-on exemption
- **WHEN** an operator later chooses default-on mode and selects an exemption
- **THEN** the saved configuration and runtime status show `all_groups=true` and the exact stable ID in `excluded_group_ids`

#### Scenario: Operator submits an unknown group ID
- **WHEN** an exemption update references a group ID that does not exist
- **THEN** Sub2API rejects the update without changing the active moderation configuration

### Requirement: Complete request-side semantic scope is explicit
The first rollout SHALL audit the complete ordered request-side semantic text extracted by Sub2API, including historical roles and tool traffic, and SHALL NOT claim model-output, image-pixel, audio, or general multimodal moderation.

#### Scenario: Dangerous history followed by a benign continuation
- **WHEN** a request contains dangerous historical instructions and the last user message is only `Continue`, or the request ends in assistant/tool traffic
- **THEN** Sub2API derives the complete deterministic chunk set, blocks on any cached unsafe prefix, and reviews every uncached chunk before any downstream account is selected

#### Scenario: Long context exceeds the former cutoff
- **WHEN** relevant content appears after the former 12,000-rune boundary
- **THEN** Sub2API includes it in the moderation request and never treats a silently truncated prefix as the complete input

#### Scenario: Operator reviews rollout status
- **WHEN** moderation coverage is displayed or documented
- **THEN** it identifies the audited direction and unsupported content types

### Requirement: Incremental review preserves full-context coverage
When `incremental_cache_enabled=true`, Sub2API SHALL split the complete
normalized transcript into deterministic overlapping chunks, SHALL reuse only
version-compatible Redis verdicts, and SHALL send every cache miss to the
moderation adapter under one overall deadline.

#### Scenario: Existing transcript receives an appended turn
- **WHEN** completed prefix chunks have unexpired safe verdicts and only the transcript tail changes
- **THEN** Sub2API sends only changed tail chunks to the adapter while requiring every prefix and tail chunk to have a strict verdict

#### Scenario: Cached unsafe history receives `Continue`
- **WHEN** any version-compatible historical chunk is cached as unsafe and a later request appends a benign continuation
- **THEN** Sub2API blocks locally without invoking MiniMax or selecting a downstream account

#### Scenario: Redis cannot prove the complete verdict set
- **WHEN** the chunk cache is unavailable or a required read or write fails in scoped `pre_block`
- **THEN** Sub2API returns a local 503 and does not select or call a downstream account

#### Scenario: Structured image is present
- **WHEN** incremental review receives a structured image that the configured text backend cannot inspect
- **THEN** Sub2API blocks locally without calling MiniMax or OAI

### Requirement: Human-controlled staged activation
The rollout MUST use an isolated candidate before replacing the active version,
and only an operator MAY activate the versioned image after reviewing quality,
latency, availability, scope, and failure-drill evidence. The system MUST NOT
promote automatically.

#### Scenario: Candidate evidence is incomplete
- **WHEN** long-context, appended-tail, scope, connectivity, or failure-drill gates are incomplete
- **THEN** the existing production image and disabled incremental switch remain active

#### Scenario: Operator approves blocking
- **WHEN** all recorded gates pass and the operator explicitly approves promotion
- **THEN** the versioned candidate is activated, allowed and blocked probes are run, and high-risk/out-of-scope behavior is reverified

### Requirement: Hosted-model quality and performance evidence
The MiniMax-first rollout MUST measure its selected hosted model on representative
risky inputs and long-context traffic before activation.

#### Scenario: Model benchmark completes
- **WHEN** the MiniMax candidate has been tested
- **THEN** the decision record contains model revision, service tier, risky-prompt outcomes, cold/appended latency, timeout, retry count, and the operator-approved selection

### Requirement: Observe is non-blocking and pre-block fails closed
The first rollout SHALL keep `observe` non-blocking, SHALL keep deterministic keyword blocking available through `keyword_and_api`, and SHALL terminate scoped `pre_block` requests locally when semantic moderation cannot produce a valid decision after bounded attempts.

#### Scenario: Pre-block sampling is configured below 100 percent
- **WHEN** a request is in the configured `pre_block` group and model scope
- **THEN** Sub2API audits it regardless of `sample_rate`, because enforcement mode cannot sample away a safety decision

#### Scenario: Adapter is unavailable for an in-scope request
- **WHEN** semantic moderation for an approved high-risk-group request times out or returns an error
- **THEN** Sub2API makes at most three total attempts, returns a local 503 if no valid decision is produced, records the error, and does not select or call a downstream account

#### Scenario: Observe adapter failure is not misreported as safe
- **WHEN** an observe-mode request continues because semantic moderation failed
- **THEN** dashboards and run evidence classify it as an audit error rather than a successful safe classification

### Requirement: Configuration snapshot and rollback
Operators MUST capture the pre-change Sub2API moderation configuration and MUST be able to restore it or return the rollout to `observe` or `keyword_only` without restarting Sub2API.

#### Scenario: Blocking regression occurs
- **WHEN** false positives, latency, adapter instability, or group-scope drift exceed the accepted gate
- **THEN** the operator restores the captured configuration, verifies normal model requests, and records the rollback reason and time

### Requirement: Boundary-specific acceptance
Local adapter tests, private connectivity, observation, and active blocking SHALL be recorded as separate evidence boundaries and MUST NOT substitute for one another.

#### Scenario: Adapter contract tests pass
- **WHEN** only local adapter and fixture tests have passed
- **THEN** the change may claim local contract readiness but not Windows readiness, deployed Sub2API integration, or production enforcement

#### Scenario: Live high-risk-group request is blocked
- **WHEN** an approved unsafe in-scope probe receives the configured Sub2API block response in `pre_block`
- **THEN** the run log records active high-risk-group enforcement and separately records the equivalent out-of-scope result
