# Welsir AI Domain Design

**Date:** 2026-07-22

## Decision

The new Sub2API service is publicly named **Welsir AI** and uses the existing
`welsir.com` domain. Its public hostname is `ai.welsir.com`.

## Public contract

- Web application: `https://ai.welsir.com`
- OpenAI-compatible API Base URL: `https://ai.welsir.com/v1`
- Legacy web and API service: `https://omni.welsir.com`
- Storefront: `https://omni.tmlab.store`

Marketing material may display only `ai.welsir.com`. Configuration guides must
use the complete `/v1` Base URL where the client expects an OpenAI-compatible
API prefix.

## Architecture

Nginx terminates TLS for `ai.welsir.com` and proxies the complete site to the
isolated V2 application on `127.0.0.1:18084`. The V2 application, PostgreSQL,
Redis, user data, balances and API keys remain separate from Legacy. Existing
Legacy routes and the `omni.welsir.com` server block are unchanged.

Internal resource names remain `sub2api-v2`, `sub2api_v2` and
`/data/sub2api-v2`. Public branding does not require a redeployment or internal
renaming.

## User experience

New users register, sign in, create keys and view billing only on Welsir AI.
Existing users continue using the Legacy hostname and credentials. A future
manual migration creates a new V2 user and key, verifies it, then retires the
old key; no account is silently moved between databases.

## Safety and verification

1. DNS for `ai.welsir.com` must resolve to `43.199.92.179` before TLS issuance.
2. A dedicated Nginx server block proxies only to `127.0.0.1:18084`.
3. `nginx -t` must pass before every reload.
4. HTTPS validation covers the page, static assets, login, `/v1/models`,
   non-stream Responses, streaming Responses and tool calls.
5. Legacy health, restart counts and business counters must remain unchanged.
6. Rollback disables only the Welsir AI Nginx entry and leaves Legacy running.
