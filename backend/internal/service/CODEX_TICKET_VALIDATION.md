# Codex ticket validation and routing

Ticket collection is an empirical compatibility mechanism, not an official model-quality guarantee. A state string's length alone does not prove that the upstream served the requested model.

## Admission

Version 2 accepts a collected state only when:

- Its Base64 envelope, timestamp, and account-specific shape pass validation. Personal/Pro defaults to 10 blocks (292 characters); Team/Business uses 12 blocks (332 characters). Account plan metadata is a hint, not proof of entitlement.
- The bounded SSE response finishes successfully with `response.completed`, `status=completed`, and the exact requested `response.model`. A conflicting model, failed event, malformed/truncated stream, or missing model rejects the probe.
- The ticket is bound to the account credential and the selected configured proxy. Ticket expiry derives from its envelope timestamp and is capped at one hour, with a 30-second safety margin.

Old tickets lack this verification record and are not reused. `target_length` is deprecated and no longer changes admission; in particular, setting it to 312 cannot make a rejected personal state eligible.

## One-time candidate configuration

```yaml
gateway:
  openai_codex_ticket:
    enabled: true
    fail_closed: true
    models: [gpt-6-astra, gpt-5.6-sol]
    harvest_proxy_url: "socks5h://USER:PASSWORD@FIRST_HOST:PORT"
    harvest_proxy_urls:
      - "socks5h://USER:PASSWORD@SECOND_HOST:PORT"
      - "socks5h://USER:PASSWORD@THIRD_HOST:PORT"
    compact_proxy_url: "socks5h://USER:PASSWORD@STABLE_HOST:PORT"
    account_probes_per_minute: 8
    model_probes_per_minute: 4
    cold_wait_seconds: 90
    max_probes_per_round: 4
    harvest_probe_interval_seconds: 8
    harvest_attempt_timeout_seconds: 25
    ttl_seconds: 3600
    refresh_before_seconds: 900
```

Keep real credentials in protected deployment configuration. The existing panel proxy replaces `harvest_proxy_url`; `harvest_proxy_urls` is an additional server-configured candidate list, not a remotely downloaded subscription. It accepts at most 32 unique validated HTTP(S)/SOCKS5(h) proxy URLs. Invalid configuration is rejected as a whole. No missing proxy is silently replaced with direct egress.

Collection uses one shared budget per upstream account inside Sub2. Defaults are at most eight starts per account in any rolling minute and at most four per model, at least 7.5 seconds between account starts / 15 seconds for the same model, and at most one in-flight probe. Models and proxy routes cannot reset the budget; failed network attempts also count. Token refresh does not reset it. Model order rotates, and missing active tickets precede reserve collection. The default scan waits eight seconds after finishing; slow requests therefore reduce the actual rate. `max_probes_per_round` cannot override the shared budget. These user-selected rates are not verified upstream safety thresholds. Only Astra and Sol are prewarmed by default.

HTTP 401/403 stops the current credential until it changes or the process restarts; 429 waits at least three minutes and honors a longer `Retry-After`. Stream rate-limit errors also stop collection. Existing account cooldowns are respected. These pauses are local to the process; this is not a distributed quota coordinator. Restarting a process is not a rate-limit recovery procedure.

Formal requests using a verified state use its selected collection proxy. Removed routes and changed credentials invalidate tickets. An identical proxy URL does not guarantee a stable physical IP: configure sticky sessions with sufficient lifetime at the provider.

Ticket-managed accounts use HTTP upstream forwarding. Client WebSocket requests use the existing HTTP bridge, allowing every turn to update its state and route instead of silently retaining an old WebSocket handshake. Explicitly disabled WebSocket ingress stays disabled. Other accounts retain their transport policy.

## Failure policy and evidence

`fail_closed: true` prevents every model request on ticket-managed OAuth accounts without its own verified ticket, for generation. Compaction is treated separately. `models` controls background prewarming only; an unlisted model stays blocked until configured and verified. API-key accounts are outside this feature. `false` explicitly permits the existing no-ticket path and emits `forwarding_without_verified_ticket`; this mode cannot promise the requested model. Authentication and quota pauses are not overridden by fail-open.

`harvested` now means the collection response passed admission. `injected` records the account, requested model, length, and short state/route digest; it never records the state or proxy credentials. Injection is not a guarantee that a later upstream response will retain the model. Strict mode also buffers each formal response (maximum 32 MiB) and validates its completed status and exact model before exposing success. Missing or conflicting model, malformed/truncated response, upstream failure, or size overflow returns an error without exposing the buffered content. This deliberately delays all streaming output until completion and increases per-request memory usage; client timeouts may need adjustment. Compaction retains its existing protocol validation instead of requiring the generation response model/state shape. Matching upstream metadata is still not independent proof of model internals or reasoning quality.

Production acceptance requires a newly verified ticket, evidence of injection, a complete response, and the requested actual model. Unit tests and an HTTP 200 do not replace this check. Do not deploy with the old `fail_closed: false` setting and call continued Luna responses a successful Astra fix.

## Reserve, waiting, and compaction

Each account/model keeps one active ticket and one newer reserve, persisted together in account Extra. Repeated copies of the same state do not renew it. Promotion retains the reserve's original issue/expiry times. Valid records are reloaded after restart; changing scan frequency does not clear them. Credential/route changes still invalidate affected records. No large stockpile is collected.

With `cold_wait_seconds: 90`, configured-model generation can wait up to 90 seconds for the shared background harvester. Waiters never launch additional probes, respect cancellation and account pauses, and fail closed when time runs out. This is a grace period, not a promise of collecting a ticket within 90 seconds. Zero disables waiting; values above 90 are capped. Active and reserve collection retains account authentication/rate-limit stops.

Explicit `/responses/compact` and native compaction-trigger requests do not inject generation state. They use the named `compact_proxy_url`, regardless of candidate list order, and keep the existing compaction response/error handling. The explicit value `direct` selects server direct egress. Missing/invalid compact proxy configuration returns an error rather than choosing a random harvest route or direct egress. Authentication/quota pauses still apply. Configure a stable proxy reachable from the Sub2 server; a local Mac loopback URL is not a server proxy.
