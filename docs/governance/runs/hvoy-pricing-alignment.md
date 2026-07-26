# HVOY Pricing Alignment Run Log

## Scope

- Branch base: `dev/omni@971830b9`.
- Isolated branch: `codex/hvoy-pricing-match`.
- Runtime target: 43 new Omni service at `ai.welsir.com` / port `18084` only.
- Old `omni.welsir.com` service is excluded from configuration and deployment
  changes.

## Pre-change evidence

- Public endpoint returned HTTP 200, schema 1.1, `site_name=Omni`, stale
  `site_domain=omni.welsir.com`, ten rows, and only `group_name=gpt01`.
- Live published group row: numeric ID `10`, internal name
  `pro20x号池[自营官方渠道]`, publication code `gpt01`, token multiplier `0.2`,
  image multiplier `0.2`.
- HVOY support evidence identifies `pro专属` as the detected group name and
  requires the price-feed `group_name` to match it.
- Provider pricing uses the same `BillingService` and dynamic
  `/app/data/model_pricing.json` source as real text billing.
- The deployed billing calculation applies `0.2` uniformly and lacks the
  required cache-read floor, producing cache prices below CNY 0.12/M.
- No API key named for HVOY/Maurice/probe/test was present; the HVOY-side probe
  name is therefore external matching metadata, not an internal database group
  rename target.

## Implementation evidence

- Added a provider-only external-name mapping. The configured internal
  publication code remains `gpt01`; the HVOY response projects it as `pro专属`.
- The provider response DTO remains schema 1.1 and does not add an unsupported
  `group_id` field.
- Changed the new Omni branch default site domain to `ai.welsir.com` and added
  an explicit JSON environment override for future external group mappings.
- Added the CNY 0.12/M cache-read floor inside the shared token billing core,
  only for the `0.2` token rate. Normal input/output, cache creation, image
  billing, and other group rates retain their existing calculation paths.
- Provider pricing continues to call `BillingService`, so its projected text
  prices and real usage charges use the same dynamic model-pricing source.
- TDD RED evidence:
  - mapping test initially failed because `GroupNameMappings` did not exist;
  - provider projection test initially failed because the constructor had no
    mapping argument;
  - the existing dynamic-source test then reported actual cache price `0.12`
    against its old `0.10` expectation.
- Verification:
  - provider/config/cache-floor targeted tests: passed;
  - `env -u OPENAI_API_KEY go test ./...`: passed;
  - `go build -tags embed -o /tmp/sub2api-hvoy-pricing-alignment-darwin ./cmd/server`: passed;
  - `git diff --check`: passed.

## Deployment and acceptance

Pending.

## Rollback

Pending final image and backup identifiers.
