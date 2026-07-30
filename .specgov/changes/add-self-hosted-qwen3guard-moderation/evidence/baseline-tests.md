# Baseline Test Evidence

Date: 2026-07-30

## Historical Adapter Source Baseline

The adapter contract was initially implemented and reviewed in
`tml-omni-gateway`, which was an incorrect ownership assumption rather than a
runtime requirement. That placement was never pushed or deployed and was
removed by TML commit `a74b1b2`.

The migrated Sub2API package retains byte-equivalent runtime and test content
from the reviewed adapter revision. Historical TML full-repository failures are
not used as Sub2API acceptance evidence.

## Sub2API dev/omni

Commands:

- `go test ./internal/service -run 'ContentModeration' -count=1`
- `pnpm exec vitest run src/views/admin/__tests__/RiskControlView.spec.ts`

Result:

- Focused backend content-moderation tests passed.
- Risk-control view tests passed: 4/4.
- The target branch baseline was clean at local commit `6c9d0eb40`; local `dev/omni` was ahead of its remote by 5 commits and behind by 2, and no pull or rebase was performed.
- Adapter ownership was corrected in Sub2API commit `79b046f82`; no company, team, main, or production state was changed.
