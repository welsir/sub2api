# Hvoy Provider Pricing API Run Log

## Scope

- Branch base: `dev/omni@eb3e0da7`
- Isolated delivery branch: `codex/hvoy-provider-pricing`
- Runtime target: 43 server, V2 stack only
- External group contract: current internal group id 10 -> `gpt01`; later groups use `gpt02`, `gpt03`, ...
- Security: mandatory timestamped HMAC-SHA256; 60-second skew; public Hvoy test secret excluded from production

## Implemented

- Added group-backed dynamic publication settings and a partial unique external-name index.
- Added admin create/edit controls for Hvoy publication, stable group ID, and explicit model list.
- Added provider-pricing projection through the existing billing source of truth and current group multiplier.
- Added schema 1.1 `GET /api/provider/pricing` with constant-time HMAC validation.
- Added disabled-by-default configuration and startup validation for a strong production secret.

## Verification before deployment

- `go test ./migrations -run TestProviderPricingMigrationAddsDynamicPublishedGroupFields -count=1`: passed.
- `go test ./internal/service -run TestProviderPricingService -count=1`: passed.
- `go test ./internal/config -run TestLoadProviderPricing -count=1`: passed.
- `go test ./internal/handler -run TestProviderPricingHandler -count=1`: passed.
- `env -u OPENAI_API_KEY go test ./...`: passed.
- `go build ./cmd/server`: passed after the frontend production build.
- `pnpm test:run`: passed, 142 files and 923 tests.
- `pnpm typecheck`: passed.
- `pnpm build`: passed; only the repository's existing dynamic-import and chunk-size warnings remained.
- `git diff --check`: passed.

The optional `go test -tags unit ./internal/service ...` gate is blocked on the branch baseline by `effectiveSubRepoStub` missing `ExistsActiveByUserIDAndGroupID` in `api_key_effective_group_test.go`. The default full test suite is green, and this feature does not modify that stub or interface.

## Deployment evidence

- Landed and pushed on `dev/omni` at `5e7beff5` before the live cutover.
- Remote build target: `tml/sub2api:v0.1.152-omni-hvoy-pricing-20260724` (`sha256:d532ba24f0059b8fb9285b99bb5e4229b889fa45c49b02216df589564573b1a2`).
- The first remote Docker build stopped safely when `/mnt` filled. With explicit operator approval, only unused Docker builder cache was pruned (`784.5 MB`); no image or container was deleted.
- The final Linux amd64 binary was cross-built from `5e7beff5`, copied into a container derived from the previous V2 image, and committed as the target image. The staging container was retained.
- Backups created before mutation:
  - `/data/sub2api-v2/docker-compose.yml.pre-hvoy-5e7beff5-20260724`
  - `/data/sub2api-v2/.env.pre-hvoy-5e7beff5-20260724`
  - `/etc/nginx/conf.d/omni-welsir-sub2api.conf.pre-hvoy-20260724`
- Production configuration enables provider pricing with site `Omni`, domain `omni.welsir.com`, a server-generated 64-hex-character HMAC secret, and a 60-second timestamp window. The secret was never printed.
- V2 group id 10 (`pro号池[自营官方渠道]`, current multiplier `0.2000`) publishes externally as stable group `gpt01` with ten configured GPT/Codex model IDs.
- Nginx routes only exact path `/api/provider/pricing` to V2 on `127.0.0.1:18084`; existing default, Codex gateway, V1, and Preview routes were left unchanged.
- Live public probes against `https://omni.welsir.com/api/provider/pricing`:
  - unsigned: `401`
  - malformed signature: `401`
  - correctly signed but 61-second-stale timestamp: `401`
  - valid production signature: `200`
- The valid response reported schema `1.1`, `CNY`, `per_1m_tokens`, ten rows, and only external group `gpt01`. The first live row was `gpt-5.6` with input `1`, output `6`, cache read `0.1`, and cache write `1.25` CNY per 1M tokens.
- Final runtime checks: `sub2api-v2-app` is healthy on the new image, public `/health` returns `{"status":"ok"}`, and `/mnt` has approximately `751 MB` free.

## Rollback boundary

Rollback must restore only the previous V2 app image and the backed-up compose, environment, and Nginx files. V1 containers and data are outside this change. Migration 176 is additive; leaving its disabled/default columns in place is safe during an image rollback.
