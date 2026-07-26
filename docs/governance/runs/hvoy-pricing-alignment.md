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

- Delivery commit `ae2f001f` was pushed as a fast-forward to `origin/dev/omni`;
  no other business branch was changed.
- Linux amd64 embedded binary SHA-256:
  `6a2fe2b3e41a1e2979a0e99869c1f0f980c4ca5cee94ae86011dc3e378ca6b4b`.
- Deployed image: `tml/sub2api:v0.1.154-omni-hvoy-align-20260726`
  (`sha256:bb166526c44cc72ae46d8ec544059ed2323e381514ffb1c770689c9e420daec1`).
- Backups created before the cutover:
  - `/data/sub2api-v2/docker-compose.yml.pre-hvoy-align-ae2f001f-20260726`
  - `/data/sub2api-v2/.env.pre-hvoy-align-ae2f001f-20260726`
- Only compose service `app` was recreated with `--no-deps`. PostgreSQL,
  Redis, gateway, preview, old-service routes, Nginx configuration, and data
  were not restarted or edited.
- Public `GET https://ai.welsir.com/api/provider/pricing` returned HTTP 200,
  schema 1.1, `site_name=Omni`, `site_domain=ai.welsir.com`, ten rows, and only
  `group_name=pro专属`.
- The public body contains no `gpt01`, internal runtime label, or
  `omni.welsir.com`. Every cache-read price is CNY `0.12/M` or greater.
- All ten published input/output prices exactly matched the active dynamic
  model-pricing file at multiplier `0.2`; all ten cache-read prices matched
  `max(base * 0.2, 0.12/M)`.
- The live group row remained unchanged: ID `10`, internal publication code
  `gpt01`, token multiplier `0.2`, image multiplier `0.2`, and the same ten
  published model IDs.
- Bounded real billing probe:
  - model: `gpt-5.6-luna`;
  - usage log ID: `51642`;
  - usage: 551 input, 5 output, 3,840 cache-read, 0 cache-create tokens;
  - expected from published prices: CNY `0.00057700`;
  - recorded actual cost: CNY `0.0005770000`;
  - test-account balance delta: CNY `0.00057700`.
- The new container stayed healthy with restart count `0` across a timed
  recheck. Since-deploy log scans found zero panic/fatal and zero error-level
  entries. Final free space was about 324 MB on `/mnt` and 13 GB on `/data`;
  no image or retained container was deleted.
- `gpt-image-2` exists in the active dynamic price source and the live group
  retains image multiplier `0.2`, but the current HVOY model list and token-only
  response contract do not publish an image row. No synthetic token-to-image
  conversion was added.
- HVOY was not notified to re-fetch during this deployment.

## Rollback

Restore only the backed-up new-service compose and environment files, validate
the compose, and recreate only its `app` service:

```bash
sudo cp /data/sub2api-v2/docker-compose.yml.pre-hvoy-align-ae2f001f-20260726 /data/sub2api-v2/docker-compose.yml
sudo cp /data/sub2api-v2/.env.pre-hvoy-align-ae2f001f-20260726 /data/sub2api-v2/.env
sudo docker compose --env-file /data/sub2api-v2/.env -f /data/sub2api-v2/docker-compose.yml config --quiet
sudo docker compose --env-file /data/sub2api-v2/.env -f /data/sub2api-v2/docker-compose.yml up -d --no-deps app
```

The previous image `tml/sub2api:v0.1.153-omni-hvoy-public-20260725` and the
new staging container are retained. No database rollback is required because
this change added no migration and changed no stored group, user, key, balance,
or historical usage row.
