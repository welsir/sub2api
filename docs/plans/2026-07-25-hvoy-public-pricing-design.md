# Hvoy Public Provider Pricing Design

## Status

Accepted on 2026-07-25.

## Context

The deployed Hvoy Provider Pricing API currently requires timestamped HMAC-SHA256 authentication. Hvoy requested that the production feed be accessible by URL without a shared secret. The endpoint is read-only and exposes only the configured public model, group, and pricing projection.

## Decision

Keep HMAC support in the application, but make enforcement explicit through `PROVIDER_PRICING_REQUIRE_HMAC`.

- The default remains `true` so existing and future environments do not become public accidentally.
- When `false`, `GET /api/provider/pricing` does not require or evaluate `X-Hvoy-Ts` or `X-Hvoy-Sign`.
- A strong HMAC secret is required at startup only when provider pricing is enabled and HMAC enforcement is enabled.
- The 43 V2 runtime will set `PROVIDER_PRICING_REQUIRE_HMAC=false` and remove `PROVIDER_PRICING_HMAC_SECRET` from its compose and environment files.

## Request Flow

1. If provider pricing is disabled, return `404` as before.
2. If HMAC enforcement is enabled, validate the timestamp and signature as before and return `401` on failure.
3. If HMAC enforcement is disabled, proceed directly to the existing pricing projection.
4. Return the existing schema 1.1 response with `Cache-Control: private, no-store`.

## Security Boundary

- Only the exact read-only route `GET /api/provider/pricing` becomes public.
- No admin, user API, database, Redis, shell, or SSH access is added.
- Stable external group IDs (`gpt01`, `gpt02`, ...) remain unchanged.
- Internal group names, multipliers, model lists, and prices continue to be projected dynamically.
- The removed production secret must not remain in the active V2 compose or `.env` after deployment.

## Verification

- Configuration tests prove HMAC enforcement defaults to `true`.
- Configuration tests prove an enabled public feed can start without a secret.
- Handler tests prove unsigned requests return `200` only when enforcement is disabled.
- Existing secure-mode tests continue to prove missing, malformed, stale, or incorrect signatures return `401` and a valid signature returns `200`.
- Live public acceptance verifies unsigned access returns `200`, schema version is `1.1`, all current rows use `gpt01`, and V2 health remains green.

## Deployment And Rollback

- Build and deploy a new V2-only image from `dev/omni`.
- Back up the active compose and `.env` before removing the secret.
- Keep the current HMAC-enabled image and backups for rollback.
- Rollback restores the previous image and backed-up runtime configuration; no database rollback is required.
