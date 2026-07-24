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

Pending landing on `dev/omni` and live V2 deployment.

## Rollback boundary

Rollback must restore only the previous V2 app image and its prior provider-pricing environment settings. V1 containers and data are outside this change. Migration 176 is additive; leaving its disabled/default columns in place is safe during an image rollback.
