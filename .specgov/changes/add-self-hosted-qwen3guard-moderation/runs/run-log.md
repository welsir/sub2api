## Task Identity

- Task name: Self-hosted Qwen3Guard moderation
- Date: 2026-07-30
- Repo / workspace: Sub2API `dev/omni` at `/Users/welsir/.config/superpowers/worktrees/sub2api/omni-latest-selected-customs`
- Main spec artifact: `openspec/changes/add-self-hosted-qwen3guard-moderation/specs/`
- Main plan artifact: `openspec/changes/add-self-hosted-qwen3guard-moderation/design.md`
- Main tasks artifact: `openspec/changes/add-self-hosted-qwen3guard-moderation/tasks.md`

## Delivery Summary

- Final outcome: In progress. The adapter and exact-ID exclusion slices are owned and accepted in Sub2API `dev/omni`; the mistaken TML placement is reverted. The 43 V2 version, database, active `tml` ID `18`, and redacted legacy moderation state are verified. Windows and rollout gates remain pending.
- Was the result acceptable: The two local implementation slices and the read-only 43 V2 baseline are accepted. Change-level acceptance remains blocked on powered-on Windows, disposable scope-test keys, traffic sizing, connectivity, benchmark, observation, and production enforcement evidence.
- Would this pattern be reused: Yes for cross-repository implementation with separate acceptance gates; the durable state prevented the resumed turn from treating local proof as production proof.

## Dispatch Decisions

- Representative no-delegate decision: The master repaired the OpenSpec scope from A/B inclusion to default-on moderation with one exact `tml` exemption.
- Why that no-delegate choice was reasonable: It was a cross-slice product-policy judgment and the source contract for both executors.
- Representative delegate decision: Sub2API exclusion behavior and the Qwen3Guard adapter are separate implementation slices inside one product branch.
- Why that delegate choice was reasonable: They have independent contracts and tests even though Sub2API owns both.
- Was the later delegation consistent with the earlier candidate slice/profile: Yes.
- If not, why did it change: Not applicable.

## Master Work

- What the master did locally: Stabilized OpenSpec, bootstrapped execution state, corrected the repository ownership error, migrated the adapter package into Sub2API, reverted the TML placement, and ran acceptance checks.
- Why those actions stayed local: They define shared boundaries and final acceptance.
- Where the master carried too much load: None recorded yet.

## Delegation Summary

| Round | Slot | Slice name | Why delegated | Result |
| --- | --- | --- | --- | --- |
| 1 | execute | Sub2API exact-ID exemptions | Independent Sub2API implementation on `dev/omni` | Integrated and reverified at `66465aaac12141a874981d4816d1063d16591ad6` |
| 2 | execute | Qwen3Guard Moderations adapter | Standalone package owned by Sub2API | Migrated and reverified at `79b046f82aaeae392baa0ad79df9456f7a0a4839` |
| 3 | verify | Actual-branch post-integration checks | Verify exact-ID behavior and the owned adapter package | Accepted; 72/72 adapter tests plus Sub2API backend/frontend gates |
| 4 | repair | Correct repository ownership | Remove the unsupported Gateway ownership assumption | Sub2API commit `79b046f82`; TML revert `a74b1b2` |
| 5 | evidence | Read 43 V2 deployment and moderation baseline | Resolve the real `tml` ID and separate code deployment from configuration activation | Active `tml` ID `18`; evidence in `evidence/live-43-v2-baseline.md` |

## Waste Signals

- Repeated file reading: None recorded yet.
- Repeated discussion loops: A/B scope was corrected once to exact `tml` exemption.
- Slice definition problems: Initial selected-group model could not protect future groups. The adapter was also initially assigned to TML without user authorization and had to be migrated.
- Review churn: Sub2API reviews found API/status/validation, fail-loud repository handling, runtime-draft drift, unknown-ID removal, and accessibility gaps. Adapter reviews required two repair rounds for malformed targets, URL and credential containment, bounded streams, redaction, finish reasons, timeout/cancellation classification, and shutdown deadlines. Every repair was re-reviewed before acceptance.
- Context pressure observed: Governed state was added because Windows work will continue later, but it did not prevent the initial repository-ownership assumption.

## Quality Signals

- Spec gate result: OpenSpec strict validation passed before implementation.
- Quality gate result: Sub2API backend/frontend and adapter specification/quality reviews passed; adapter ownership was corrected without changing reviewed runtime behavior.
- Command gate result: The Sub2API-owned adapter passed 72/72, typecheck, 9 checked-in JS syntax checks, content-equivalence, diff check, and scoped secret scan. Sub2API exact-ID, ContentModeration regression, handler, race, frontend 15/15, typecheck, lint, build, and diff checks passed.
- Remaining risks: Windows host is powered off; disposable scope-test keys, complete rollback-safe configuration capture, real traffic shape, model quality, Tailscale, observation, and production enforcement remain unverified.

## Retrospective

- Governance gaps observed: The integration ledger has no explicit event for creating an `awaiting_review` entry, so this workspace follows its existing accepted-snapshot convention. A future protocol revision should make that creation/reconciliation event explicit. Workspace-wide legacy `.specgov/tasks` errors remain unrelated and are not used for this change.
- What felt efficient: Keeping the adapter as a separately testable process while moving ownership into Sub2API.
- What felt expensive: The unsupported Gateway ownership assumption caused a full repository migration and revert after implementation.
- What should change next time: Bind `dev/omni` to an explicit repository before artifact creation; do not infer ownership from nearby workspace history.

## 2026-08-02 MiniMax-First Local Candidate Checkpoint

### Delivery Summary

- Final outcome: The adapter runtime, MiniMax failure mapping, complete-context protocol tests, and privacy-safe real-archive replay were integrated as one local `dev/omni` candidate. Production was not changed.
- Local acceptance: Adapter 127/127, focused Go race suites, offline `go test ./...`, `go build ./...`, Docker build/smoke, OpenSpec strict validation, diff hygiene, and secret scan passed.
- Release acceptance: Still blocked. The available MiniMax credential returned deterministic plan/quota failures, so the required real `MiniMax-M2.7-highspeed` classification, semantic replay, and hosted latency/concurrency gate remain unverified.

### Dispatch And Integration

| Round | Slot | Slice name | Why delegated | Result |
| --- | --- | --- | --- | --- |
| 6 | repair | MiniMax 502 root cause | Separate provider-code and request-shape boundary | Integrated: 2056/2062 billing mapping, high-speed model-name selection, 127/127 adapter tests |
| 7 | execute | Complete-context failure matrix | Independent Go protocol and downstream-boundary tests | Integrated: focused and race suites pass |
| 8 | evidence | Real archive inventory | Read-only aggregate corpus discovery | Accepted: 59,236 prompts and 70,231 full requests inventoried without raw-content output |
| 9 | repair | Single adapter runtime source | Remove the test/runtime JavaScript divergence that caused the partial rollout | Integrated: TypeScript compiles to `dist/`; Docker and tests run the same output |
| 10 | integrate | Privacy-safe real replay | Turn local archives into an opt-in deterministic decision-path replay | Integrated: 1,253 broad representative rows plus a separate exact-response replay of all 13 confirmed OAI `cyber_policy` cases |

### Quality Signals

- The optional OAI token-count comparison was initially enabled by the shell's `OPENAI_API_KEY` and failed independently of this change. The offline release command was corrected to `env -u OPENAI_API_KEY go test ./... -count=1`, which passed and made the no-OAI boundary explicit.
- Full-request replay used 362 moderation calls for 141 requests, showing approximately 2.57 calls per cold request in that sample. This creates a real plan-capacity risk that must be remeasured with MiniMax and production-like cache hits.
- Root-wide truth extraction first failed because HTTP 4xx was incorrectly assumed; real streamed policy failures may arrive inside an HTTP 200 response body. Exact response-token scanning found all 13 confirmed cases and drove 127 local full-context calls without exposing source content.
- A naive root-wide request-body scan timed out and reached approximately 4.1 GB RSS. The accepted design separates bounded single-archive full-request sampling from response-first truth extraction; it keeps real archive replay opt-in and out of ordinary CI.
- Loopback replay latency is local pipeline evidence only; it is not a hosted MiniMax latency claim.

### Closeout Reflection

- The master carried integration review, offline full-suite verification, privacy-safe replay integration, and the final release judgment.
- Provider repair, protocol tests, corpus inventory, and runtime-build repair were delegated as bounded slices.
- The most expensive part was discovering that tests executed TypeScript while the image executed stale JavaScript; making one compiled runtime source removed that class of half-rollout failure.
- The execution-state layer preserved the earlier local-vs-production boundary and prevented the inactive MiniMax plan from being recorded as a successful release gate.
- The workflow is worth reusing for a similar security-sensitive rollout, but production promotion must remain a separate explicit approval event.

## 2026-08-03 Attachment-Text Candidate Hardening

### Delivery Summary

- The approved V1 keeps attachments usable and forwards their original request
  representation unchanged. MiniMax receives the complete ordered visible text
  plus controlled metadata markers, never attachment bytes, raw references, file
  names, opaque IDs, credentials, or protocol headers.
- Independent review initially rejected the candidate for WebSocket, nested
  attachment, Base64, readiness concurrency, cache ordering, and configuration
  gaps. Each finding was reproduced or covered by a deterministic regression
  before repair.
- The local candidate now covers binary-JSON WebSocket follow-up frames, malformed
  text frames, nested `file`/`source`/`data`/`image_url` semantic siblings,
  Gemini attachment wrappers, OpenAI input audio, unknown content blocks, and
  top-level tool/schema text.
- Admin API and UI now persist `classifier_policy_revision` together with the
  incremental cache switch. No commit, push, deployment, or production setting
  change was performed.

### Verification

- Backend full offline suite and build: passed.
- Frontend: 148 files / 996 tests, typecheck, scoped lint, and production build:
  passed.
- Adapter: 7 files / 135 tests, typecheck, build, no-cache Docker build, and
  disposable non-root runtime smoke: passed.
- Focused race and repeated concurrency suites: passed.
- Strict OpenSpec, governance state, and diff hygiene: passed.

### Release Boundary

- The local implementation is reviewable as one candidate package, but release
  remains blocked on a real `MiniMax-M2.7-highspeed` classification and semantic
  replay, measured hosted latency/concurrency, Windows/private connectivity if
  selected, isolated-candidate probes, rollback rehearsal, and explicit operator
  approval.
- V1 still does not inspect pixels or file bytes. A benign visible prompt with a
  malicious attachment-only payload can pass and remains an explicit residual
  risk.

## 2026-08-03 Real MiniMax M3 Verification

### Delivery Summary

- The supplied Token Plan credential was injected only into a disposable local
  adapter process. It was not written to the repository, environment files, or
  evidence output, and no production service or configuration was changed.
- Plan status, model discovery, and real inference succeeded. A direct comparison
  selected `MiniMax-M3` with thinking disabled over
  `MiniMax-M2.7-highspeed`, which still emitted reasoning tokens and was slower
  on the classifier contract.
- A Unicode-unit mismatch at the 32,768-code-point chunk boundary was reproduced
  with a red test and repaired before the affected real archives were replayed.

### Verification

- Adapter: 7 files / 136 tests, typecheck, and build passed.
- Backend: offline `go test ./... -count=1` and `go build ./...` passed.
- OpenSpec strict validation, JSON parsing, and diff hygiene passed.
- All 13 exact archived OAI `cyber_policy` requests blocked through the real
  adapter after the Unicode repair, with zero allows and zero errors.
- A 40-row privacy-safe real sample completed without provider errors. Its cold
  latency was P50 1.866 seconds, P95 8.262 seconds, and maximum 14.939 seconds.
- The final temperature-zero v4 probe matched 4 of 4 expected decisions at 1.489
  to 3.070 seconds per call.
- The v5 classic-wrapper regression preserved risky earlier history despite a
  benign suggestion-generation outer task and final turn. Real M3 blocked the
  risky history in 2.035 seconds and allowed a benign-history control in 1.407
  seconds.
- That regression found and repaired a legacy-path inconsistency: non-incremental
  review now respects provider `flagged=true` as well as configured score
  thresholds, matching the incremental path.

### Release Boundary

- Production remains blocked. Cold provider latency exceeds the requested
  one-second target, the complete 1,200-to-1,400 semantic replay is unfinished,
  the individual Token Plan does not prove multi-user production capacity, and
  isolated image, rollback, and real client-boundary gates have not run.
- The next production-shaped slice is one clean isolated candidate plus capacity,
  cache-hit, throttling, and rollback evidence, followed by explicit operator
  approval. There was no deployment in this checkpoint.
