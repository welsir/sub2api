# Live 43 V2 Baseline Evidence

Date: 2026-07-30

## Boundary

- Target: saved SSH host `omni-shop-43-199-92-179`.
- Access path: audited `tml-ssh-ops` wrapper with verified host fingerprint.
- Operation type: read-only Docker and PostgreSQL inspection.
- No database write, setting update, restart, deployment, secret output, or traffic mutation was performed.

## Verified Runtime

- App container: `sub2api-v2-app`, healthy.
- App image: `tml/sub2api:v0.1.156-omni-luna-first-text-20260728`.
- PostgreSQL container: `sub2api-v2-postgres`, healthy.
- Database and role: `sub2api_v2`.
- Redis container: `sub2api-v2-redis`, healthy.

## Verified Group Identity

The active exact-name row is:

- `id=18`
- `name=tml`
- `status=active`
- `deleted_at=NULL`

Two other name matches are not eligible for the exemption:

- ID `8`, `TML Global Pool`, soft-deleted on 2026-07-14.
- ID `9`, `TML Prox20 Single Account Test`, inactive and soft-deleted.

The rollout must therefore use exact group ID `18`, not display-name matching.

## Redacted Live Moderation State

- Global `risk_control_enabled=true`.
- Content moderation `enabled=true`.
- Mode: `pre_block`.
- Scope: `all_groups=false`, `group_ids=[8,10,13,14,15]`.
- `excluded_group_ids` is absent from the legacy JSON.
- Keyword mode: `keyword_only`.
- Semantic-provider Base URL: absent.
- Semantic-provider API credential: absent.

## Deployment Impact

Shipping the feature code without changing the live setting preserves the
legacy selected-group scope: a missing `excluded_group_ids` field normalizes to
an empty list, and `all_groups=false` continues to use `group_ids`.

The standalone adapter package is not referenced by the Sub2API app runtime and
does not start or consume resources unless it is separately deployed.

Changing the setting is a separate production action. Setting
`all_groups=true` with `excluded_group_ids=[18]` while retaining the current
`pre_block` plus `keyword_only` mode would immediately apply deterministic
keyword blocking to every non-`tml` group. That configuration must not be
applied as part of a code-only deployment.

## Remaining Boundary

This evidence does not prove disposable-key scope behavior, Windows model
readiness, private connectivity, semantic quality, observation results, or
active Qwen3Guard enforcement.
