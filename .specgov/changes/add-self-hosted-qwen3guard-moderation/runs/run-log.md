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
