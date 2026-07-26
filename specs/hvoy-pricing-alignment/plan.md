# HVOY Pricing Alignment Plan

1. Capture the pre-change public response, runtime configuration, group row,
   dynamic model prices, health, logs, and restart count.
2. Add regression tests for external group-name mapping and the Omni cache-read
   floor; run them in RED state.
3. Implement a provider-only group mapping configuration and apply the cache
   floor inside the shared real billing calculation.
4. Run targeted and full backend tests plus an embedded production build.
5. Back up only the new service compose/env/runtime files.
6. Build and deploy a new image to `sub2api-v2-app`; do not alter old-service
   containers, data, or Nginx configuration.
7. Validate the public contract and perform a bounded real billing probe with
   before/after usage evidence.
8. Record rollback commands and final evidence in the run log.

