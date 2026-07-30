## 1. Lock Runtime And Rollout Baselines

- [x] 1.1 Verify the current Sub2API `dev/omni` source baseline, recording that the existing `all_groups` plus `group_ids` model cannot safely express "all groups except `tml`".
- [ ] 1.2 Verify the deployed Sub2API version, resolve the exact stable `tml` group ID, and prepare disposable `tml`, existing non-`tml`, newly created, and ungrouped API keys for scope verification.
  - Partial evidence (2026-07-30): 43 V2 runs `tml/sub2api:v0.1.156-omni-luna-first-text-20260728` against `sub2api_v2`, and active exact-name `tml` is group ID `18`; disposable scope-test keys remain pending.
- [ ] 1.3 Measure current inbound request peak RPS, concurrency, P95/P99 text length, and acceptable moderation latency instead of sizing only from the 50,000-per-day average.
- [ ] 1.4 Define operator-approved unsafe-recall, safe false-positive, latency, and availability gates for model selection and `pre_block`.
- [ ] 1.5 Capture and redact the current Sub2API moderation configuration as the rollback baseline.

## 2. Build The Moderation Adapter Contract

- [x] 2.1 Create the adapter module, dependency manifest, configuration schema, file headers, and local folder documentation following repository conventions.
- [x] 2.2 Implement Bearer authentication and `POST /v1/moderations` request validation for supported text inputs.
- [x] 2.3 Implement the versioned `Safe` / `Controversial` / `Unsafe` parser and deterministic Qwen-to-Sub2API category mapping with the unsafe fallback.
- [x] 2.4 Implement the OpenAI-shaped response containing `flagged`, `categories`, and `category_scores`.
- [x] 2.5 Implement `/healthz`, `/readyz`, and a model-backend client that keeps the Qwen Chat Completions surface behind the adapter.
- [x] 2.6 Add bounded input, concurrency, queue, and inference deadlines with explicit overload and timeout responses.
- [x] 2.7 Add redacted operational logs and metrics for model revision, mapping revision, correlation ID, input size/hash, label/category, latency, overload, timeout, and parse error.
- [x] 2.8 Add contract fixtures for safe, controversial, mapped unsafe, unknown-category unsafe, malformed output, unsupported multimodal input, authentication failure, timeout, and overload.
- [x] 2.9 Run adapter unit, contract, lint/type, secret-scan, and `git diff --check` gates and record exact results.

## 3. Add Sub2API Group Exemptions

- [x] 3.1 Add normalized `excluded_group_ids` to the Sub2API moderation config, update input/view DTOs, and preserve backward-compatible defaults for stored configurations.
- [x] 3.2 Validate every excluded group ID through the existing group repository and reject invalid updates without changing active configuration.
- [x] 3.3 Update group-scope evaluation so `all_groups=true` audits every request except an exact excluded non-null group ID, including auditing requests with no group ID.
- [x] 3.4 Preserve existing `all_groups=false` plus `group_ids` allowlist semantics and reject or normalize ambiguous include/exclude combinations.
- [x] 3.5 Add admin API, runtime status, structured log, and management-UI support for selecting and displaying explicit exempt groups.
- [x] 3.6 Add backend and frontend tests for the exact `tml` ID, same-name different-ID groups, existing non-`tml` groups, newly created groups, missing group IDs, nonexistent exclusions, empty exclusions, and legacy allowlist mode.
- [x] 3.7 Update the Sub2API file headers and affected folder documentation required by its repository protocol.
- [x] 3.8 Run focused Sub2API service, handler, admin API, frontend, type, and build gates and record the exact Sub2API commit/version required by Sub2API deployment.

## 4. Prepare The Windows Qwen3Guard Runtime

- [ ] 4.1 Power on the target Windows host and record Windows, WSL2, NVIDIA driver, GPU/VRAM, CUDA, Python, and container/runtime versions.
- [ ] 4.2 Prove WSL2 GPU visibility and select vLLM/SGLang or the documented Transformers fallback through a reproducible compatibility check.
- [ ] 4.3 Load the pinned Qwen3Guard-Gen 0.6B revision and record cold start, warm inference, memory use, and structured-output fixtures.
- [ ] 4.4 Load the pinned Qwen3Guard-Gen 4B revision under the same limits and record equivalent evidence or a concrete compatibility failure.
- [ ] 4.5 Bind the model backend to loopback, configure the adapter with external secret inputs, and prove the model backend is not tailnet- or internet-accessible directly.
- [ ] 4.6 Configure deterministic startup for Tailscale, WSL2, the model backend, and the adapter; disable sleep/hibernation for the declared service window.
- [ ] 4.7 Verify recovery after Windows reboot, WSL restart, model-process restart, and broadband reconnect.

## 5. Establish Private Connectivity

- [ ] 5.1 Create a least-privilege Tailscale ACL allowing only the Sub2API host identity to reach the adapter port.
- [ ] 5.2 Provision and rotate a dedicated adapter Bearer secret without committing or printing it.
- [ ] 5.3 Prove approved and denied tailnet identities behave as specified and confirm there is no public adapter or model-server ingress.
- [ ] 5.4 Run authenticated `/healthz`, `/readyz`, safe, and unsafe probes from the actual Sub2API container or production-equivalent network namespace.
- [ ] 5.5 Measure tailnet latency, inference latency, timeout behavior, and reconnect behavior from the Sub2API runtime boundary.

## 6. Benchmark And Select The Model

- [ ] 6.1 Build a privacy-reviewed representative set of 500 to 1,000 labeled Chinese inputs covering normal, controversial, violent, illegal, sexual, self-harm, PII, political, copyright, jailbreak, and nuanced legal content.
- [ ] 6.2 Run 0.6B and 4B against the identical labeled set and record per-category misses, unsafe recall, and safe false-positive rate.
- [ ] 6.3 Replay the measured peak concurrency and P95/P99 input shape against both candidates and record latency percentiles, throughput, queue pressure, errors, and GPU memory.
- [ ] 6.4 Select and pin the production candidate only after comparing quality and runtime evidence against the approved gates.
- [ ] 6.5 Record the selected model revision, adapter revision, mapping revision, input limit, timeout, retry count, and rejected alternative.

## 7. Run The Default-On Observation

- [ ] 7.1 Configure Sub2API with the adapter origin, dedicated Bearer secret, selected model identifier, bounded timeout/retry values, `all_groups=true`, and exactly group ID `18` in `excluded_group_ids`.
- [ ] 7.2 Set the rollout to `observe` plus `keyword_and_api` and verify no automatic promotion mechanism is active.
- [ ] 7.3 Send equivalent safe and unsafe probes through disposable `tml`, existing non-`tml`, newly created, and ungrouped keys; record only `tml` bypassing local moderation.
- [ ] 7.4 Confirm a same-name different-ID group is audited and deleting/recreating `tml` does not transfer the exemption automatically.
- [ ] 7.5 Confirm image, output, and unsupported multimodal requests are not reported as covered by this inbound-text rollout.
- [ ] 7.6 Collect an agreed observation window of real latency, categories, errors, timeouts, parse failures, fail-open allowances, audited-group counts, and `tml` exemption counts.
- [ ] 7.7 Review false positives and false negatives from observation and rerun the model-selection gate if the evidence disagrees with the labeled benchmark.

## 8. Exercise Failure Policy And Promote Manually

- [ ] 8.1 Drill adapter stop, model stop, malformed model output, tailnet loss, Windows reboot, queue overload, and recovery while non-`tml` traffic remains in `observe`.
- [ ] 8.2 Prove semantic-provider failures are recorded as errors rather than safe classifications and verify configured hard keywords still follow deterministic blocking policy.
- [ ] 8.3 Verify that retries and timeouts remain within the accepted user-latency budget during a complete home-host outage.
- [ ] 8.4 Present quality, performance, observation, exemption, failure, rollback, and remaining fail-open risk evidence for explicit operator approval.
- [ ] 8.5 After approval, switch non-`tml` traffic to `pre_block` and run approved safe, semantic-unsafe, keyword-unsafe, adapter-error, new-group, ungrouped, and `tml`-exemption probes.
- [ ] 8.6 Verify blocked and allowed responses at the real client boundary, correlate Sub2API and adapter evidence, and confirm only `tml` skips this local moderation layer.
- [ ] 8.7 Monitor the initial blocking window and roll back immediately if exemption scope, latency, availability, or false-positive gates regress.

## 9. Close Documentation And Verification

- [ ] 9.1 Add operator documentation for installation, startup, health, secret rotation, model change, `tml` exemption management, observe/block promotion, failure semantics, and rollback.
- [ ] 9.2 Update the repository README and affected documentation indexes only where the implemented runtime or documented operator entrypoints actually change.
- [ ] 9.3 Record a governance run log that separates adapter proof, Sub2API exclusion proof, Windows proof, private-connectivity proof, Sub2API observation, and production blocking evidence.
- [ ] 9.4 Run strict OpenSpec validation, targeted adapter and Sub2API tests, deployment-config validation, authenticated smoke probes, and rollback rehearsal.
- [ ] 9.5 Leave any unverified Windows, deployed-version, exclusion, model-quality, or production gate unchecked and state the remaining blocker explicitly.
