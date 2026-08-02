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

### Requirement: Only `tml` is exempt
The 43 V2 rollout SHALL place exactly the verified active `tml` group ID `18` in `excluded_group_ids` and SHALL NOT exempt any other group.

#### Scenario: Request uses the `tml` group ID
- **WHEN** an inbound request authenticates with an API key bound to the explicitly exempt `tml` group ID
- **THEN** Sub2API skips this local content-moderation path and records an explicit group-exemption reason

#### Scenario: Group merely has a similar display name
- **WHEN** a different group is named `tml`, differs only by case, or otherwise resembles the exempt group's display name but has another ID
- **THEN** Sub2API moderates it because exemptions are matched by stable ID rather than runtime name comparison

#### Scenario: Exempt group is deleted and recreated
- **WHEN** the original `tml` group is deleted and another group is created with the same name but a new ID
- **THEN** the new group is moderated until an operator explicitly updates the exemption after verifying its identity

### Requirement: Exemption configuration is visible and validated
The Sub2API admin API, configuration view, runtime status, and management UI MUST expose normalized excluded group IDs and MUST reject nonexistent exemption IDs.

#### Scenario: Operator saves the `tml` exemption
- **WHEN** an operator selects `tml` as the sole exemption in default-on mode
- **THEN** the saved configuration and runtime status show `all_groups=true` and exactly that verified group ID in `excluded_group_ids`

#### Scenario: Operator submits an unknown group ID
- **WHEN** an exemption update references a group ID that does not exist
- **THEN** Sub2API rejects the update without changing the active moderation configuration

### Requirement: Complete request-side semantic scope is explicit
The first rollout SHALL audit the complete ordered request-side semantic text extracted by Sub2API, including historical roles and tool traffic, and SHALL NOT claim model-output, image-pixel, audio, or general multimodal moderation.

#### Scenario: Dangerous history followed by a benign continuation
- **WHEN** a request contains dangerous historical instructions and the last user message is only `Continue`, or the request ends in assistant/tool traffic
- **THEN** Sub2API sends the complete ordered semantic history to moderation before any downstream account is selected

#### Scenario: Long context exceeds the former cutoff
- **WHEN** relevant content appears after the former 12,000-rune boundary
- **THEN** Sub2API includes it in the moderation request and never treats a silently truncated prefix as the complete input

#### Scenario: Operator reviews rollout status
- **WHEN** moderation coverage is displayed or documented
- **THEN** it identifies the audited direction and unsupported content types

### Requirement: Human-controlled staged activation
The rollout MUST begin in `observe`, and only an operator MAY promote audited non-`tml` traffic to `pre_block` after reviewing quality, latency, availability, exemption, and failure-drill evidence. The system MUST NOT promote automatically.

#### Scenario: Observe evidence is incomplete
- **WHEN** labeled-sample, peak-load, connectivity, or failure-drill gates are incomplete
- **THEN** non-`tml` traffic remains in non-blocking observation and no task marks production blocking complete

#### Scenario: Operator approves blocking
- **WHEN** all recorded gates pass and the operator explicitly approves promotion
- **THEN** non-`tml` traffic is switched to `pre_block`, allowed and blocked probes are run, and `tml` exemption behavior is reverified

### Requirement: Quality and performance comparison
The rollout MUST compare Qwen3Guard-Gen 0.6B and 4B on the same representative labeled inputs and measured peak traffic shape before choosing the production model.

#### Scenario: Model benchmark completes
- **WHEN** both candidate models have been tested
- **THEN** the decision record contains model revision, unsafe recall, safe false-positive rate, latency percentiles, throughput, memory use, and the operator-approved selection

### Requirement: Observe is non-blocking and pre-block fails closed
The first rollout SHALL keep `observe` non-blocking, SHALL keep deterministic keyword blocking available through `keyword_and_api`, and SHALL terminate scoped `pre_block` requests locally when semantic moderation cannot produce a valid decision after bounded attempts.

#### Scenario: Adapter is unavailable for a non-`tml` request
- **WHEN** semantic moderation for an audited non-`tml` request times out or returns an error
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

#### Scenario: Live non-`tml` request is blocked
- **WHEN** an approved unsafe non-`tml` probe receives the configured Sub2API block response in `pre_block`
- **THEN** the run log records active default-on enforcement and separately records the equivalent `tml` exemption result
