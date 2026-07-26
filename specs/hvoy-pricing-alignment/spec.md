# HVOY Pricing Alignment Specification

## Objective

Align the public provider-pricing contract exposed by the 43 Omni service at
`https://ai.welsir.com/api/provider/pricing` with HVOY's detected group and the
service's real billing policy.

## Scope

- Target branch: `dev/omni` only.
- Target runtime: `sub2api-v2-app` behind `ai.welsir.com` on port `18084`.
- Do not modify the old `omni.welsir.com` service, its users, balances, API
  keys, permissions, historical usage, or database group IDs.
- Keep internal provider publication code `gpt01` unchanged.
- Publish HVOY matching name `pro专属` through an explicit external mapping.
- Publish `site_name=Omni` and `site_domain=ai.welsir.com`.
- Do not publish the internal runtime label `V2`.

## Pricing Contract

- Currency: CNY.
- Token unit: per 1M tokens.
- Standard input/output: the dynamically loaded model price multiplied by
  `0.2`.
- Cache read: `max(dynamic base cache-read price * 0.2, 0.12 CNY / 1M)`.
- Cache creation: dynamically loaded model price multiplied by `0.2`.
- Image multiplier remains the live group configuration value `0.2`; the
  existing HVOY feed does not publish image rows unless the implementation can
  express the system's real image billing unit without inventing a conversion.

## Acceptance

- Public GET returns HTTP 200 and schema 1.1 JSON.
- All published rows use `group_name=pro专属`.
- No public response contains `gpt01`, `V2`, or `omni.welsir.com`.
- Every non-null cache-read price is at least `0.12`.
- Selected GPT prices match the live dynamic pricing source and the actual
  billing calculation.
- A real low-cost request produces a usage record whose actual charge matches
  the published input/output/cache unit prices for its recorded token counts.
- New runtime remains healthy with no new error or restart signal.

