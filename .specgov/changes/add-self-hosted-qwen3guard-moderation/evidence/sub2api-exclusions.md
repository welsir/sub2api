# Sub2API Exact-Group Exclusion Evidence

Date: 2026-07-30

## Boundary

- Repository worktree: `/Users/welsir/.config/superpowers/worktrees/sub2api/omni-latest-selected-customs`
- Product line: Omni user edition only
- Branch: `dev/omni`
- Baseline: local Sub2API `dev/omni` commit `6c9d0eb40`
- Exact-ID implementation head: `66465aaac12141a874981d4816d1063d16591ad6`
- Current component ownership head: `79b046f82aaeae392baa0ad79df9456f7a0a4839`
- Integrated commits: `918876ade`, `bb90171d3`, `c98e8e150`, `b4bb3dfe8`, `66465aaac`, and `79b046f82`
- No push, merge, rebase, pull, production change, or company/team/main synchronization was performed.

## Accepted Behavior

- `all_groups=true` audits every request except an exact non-null ID in `excluded_group_ids`.
- Missing, newly created, same-name/different-ID, and other non-exempt groups remain in scope.
- `all_groups=false` keeps the legacy `group_ids` allowlist and clears exclusions.
- Admin API, configuration view, runtime status, logs, and the management UI expose normalized exclusions.
- Unknown IDs are rejected without persistence; missing or failing group repositories fail loudly.
- The UI keeps server runtime state separate from unsaved form edits and can remove unknown or inactive excluded IDs.

## Review Gates

- Backend specification review: approved after one repair cycle.
- Backend quality review: approved after repository error handling was made fail-loud.
- Frontend specification review: approved.
- Frontend quality review: approved after runtime snapshot, unknown-ID removal, and accessibility repairs.

## Integrated Verification

Passed on the actual `dev/omni` head:

- Exact-ID service tests: 9/9, including race.
- ContentModeration service regression: 64 top-level tests plus 5 HTTP-status subtests.
- Admin API handler: 1/1, including race.
- `pnpm exec vitest run src/views/admin/__tests__/RiskControlView.spec.ts src/i18n/__tests__/riskControlLocales.spec.ts` (`15/15`)
- `pnpm typecheck`
- `pnpm run lint:check`
- `pnpm build`
- `git diff --check 6c9d0eb40..HEAD`, `git diff --check`, and `git diff --cached --check`

The production build transformed 901 modules. It emitted existing-style Browserslist, mixed static/dynamic import, chunk-size, and Node deprecation warnings but exited successfully. Generated web assets did not leave the worktree dirty.

The only `401` observed was the passing local mock subtest `401_freezes_ten_minutes`; no live OpenAI request or external authentication failure occurred during this verification.

## Documentation Check

The Sub2API project-level `AGENTS.md` requires branch isolation and delivery evidence but does not require per-file headers or per-directory `.folder.md` updates. No `.folder.md` exists in the affected backend or frontend directories, so task 3.7 required no additional Sub2API documentation edit.

## Remaining Boundary

This evidence proves only the local Sub2API implementation and repository ownership in `dev/omni`. It does not prove the deployed Sub2API version, the production `tml` ID, Windows model readiness, Tailscale connectivity, observation results, or active blocking.
