# Hvoy Provider Pricing API Design

## Goal

Expose Omni V2's customer-facing GPT prices to Hvoy through `GET /api/provider/pricing` without coupling Hvoy's stable identifiers to mutable internal group names.

The first published internal group is `pro号池[自营官方渠道]`, exposed as `gpt01`. Future published groups receive `gpt02`, `gpt03`, and so on. Renaming an internal group or changing its multiplier must not change the external Hvoy identifier.

## Contract

- Protocol: Hvoy schema version `1.1`.
- Currency: `CNY`.
- Default unit: `per_1m_tokens`.
- One response row represents one `model_name + group_name` pair.
- Published `group_name` values use the stable format `gptNN` (`gpt01`, `gpt02`, ...).
- Prices are final customer prices after applying the current internal group multiplier.
- Group availability is projected to the Hvoy row's `enabled` field.
- `updated_at` is the UTC generation time of the response.

## Security

The endpoint requires both request headers:

- `X-Hvoy-Ts`: Unix timestamp in seconds.
- `X-Hvoy-Sign`: lowercase hex HMAC-SHA256 of the timestamp string using the configured secret.

Requests are rejected with HTTP 401 when either header is missing, the timestamp is malformed, the timestamp differs from server time by more than 60 seconds, the signature is malformed, or the signature comparison fails. Signature comparison uses constant-time equality. The secret and received signature are never logged.

The endpoint is disabled by default and startup fails when it is enabled without a sufficiently strong secret. Production does not use Hvoy's public test secret.

## Dynamic group mapping

Each internal group stores three provider-pricing fields:

- `provider_pricing_enabled`: whether the group is included in the feed.
- `provider_pricing_group_name`: the stable Hvoy identifier, such as `gpt01`.
- `provider_pricing_models`: the explicit models published for this group.

The external identifier is unique among non-deleted groups and validated as `gpt` followed by at least two digits. It is independent from the internal group `name`.

Administrators manage these fields through the existing group create/edit workflow. Changes take effect on the next fetch without a process restart. Disabling or deactivating a previously published group keeps its configured identity and returns its rows with `enabled: false`; adding `gpt02` does not modify `gpt01`.

## Price projection

A dedicated provider-pricing service reads published groups and uses the existing billing service as the source of truth. For each configured model it calculates isolated one-token input, output, cache-read, five-minute cache-write, and one-hour cache-write costs with the group's current multiplier, then scales those amounts to one million tokens.

Using one-token probes prevents long-context surcharges from being triggered by the projection itself. It also means later internal multiplier and pricing-table changes are reflected automatically. Models whose token pricing cannot be resolved are omitted and logged without failing otherwise valid rows.

The feed exposes standard token pricing only. Per-request image/video products and context-dependent surcharges cannot be represented by Hvoy's single token-price row and are therefore outside this slice.

## Components

- Ent group schema and SQL migration for the three configuration fields and a partial unique index.
- Group domain, repository, admin DTO, service input, mapper, and frontend edit controls.
- Provider-pricing service for projection and response construction.
- Public HMAC-protected handler registered at `/api/provider/pricing`.
- Configuration for enablement, site metadata, secret, and timestamp skew.
- Unit, handler, repository/schema, and frontend build verification.

## Deployment

The change is developed in an isolated worktree based on `dev/omni`, then landed on `dev/omni`. Deployment targets only the 43 server V2 stack. Before restart, preserve the current image reference and runtime configuration for rollback. Enable the endpoint with a production secret, map internal group id 10 to `gpt01`, configure the approved GPT model list, and verify health, unsigned rejection, stale-signature rejection, valid signed response, and price arithmetic against the live group multiplier.

