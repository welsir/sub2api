# Hvoy Provider Pricing API Implementation Plan

> **For Codex:** Execute this plan task-by-task with test-driven development and verify every layer before deploying.

**Goal:** Add a mandatory-HMAC Hvoy price feed whose stable `gptNN` group identifiers are configured independently from Omni's mutable internal group names and whose prices follow the current group multiplier.

**Architecture:** Store publication configuration on each group, project final token prices through the existing billing service, and expose a small public handler protected by timestamped HMAC. Reuse the existing admin group workflow so changes are dynamic and do not require a restart.

**Tech Stack:** Go, Gin, Ent/PostgreSQL, Vue 3/TypeScript, Vitest, Docker Compose.

---

## Task 1: Persist provider-pricing publication settings

**Files:**
- Modify: `backend/ent/schema/group.go`
- Create: `backend/migrations/176_add_group_provider_pricing.sql`
- Modify: `backend/migrations/migrations.go`
- Modify generated Ent group files via `go generate ./ent`
- Test: `backend/migrations/provider_pricing_migration_test.go`

1. Add a failing migration regression test for the three columns and partial unique external-name index.
2. Add `provider_pricing_enabled`, `provider_pricing_group_name`, and JSONB `provider_pricing_models` to the group schema and migration.
3. Regenerate Ent code and run the migration test.

## Task 2: Carry settings through group domain, repository, and admin API

**Files:**
- Modify: `backend/internal/service/group.go`
- Modify: `backend/internal/service/group_service.go`
- Modify: `backend/internal/repository/group_repo.go`
- Modify: `backend/internal/service/admin_service.go`
- Modify: `backend/internal/service/admin_group.go`
- Modify: `backend/internal/handler/admin/group_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Test: `backend/internal/service/admin_service_group_test.go`
- Test: `backend/internal/handler/admin/group_handler_test.go`

1. Add failing tests for `gptNN` validation and create/update propagation.
2. Add fields to the service group and repository mapping.
3. Validate that enabled publication uses an OpenAI group, a valid stable external name, and a non-empty normalized model list.
4. Carry the fields through admin request/response types and rerun targeted tests.

## Task 3: Build the provider-pricing projection service

**Files:**
- Create: `backend/internal/service/provider_pricing_service.go`
- Create: `backend/internal/service/provider_pricing_service_test.go`
- Modify: `backend/internal/service/group_service.go`

1. Add failing tests for multiple stable groups, live multiplier application, inactive rows, cache fields, ordering, and unresolved-model omission.
2. Add a repository method that lists every published non-deleted group regardless of active status.
3. Implement the schema 1.1 response and isolated one-token price probes scaled to one million tokens.
4. Run the provider-pricing service tests.

## Task 4: Add configuration and mandatory HMAC handler

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Create: `backend/internal/handler/provider_pricing_handler.go`
- Create: `backend/internal/handler/provider_pricing_handler_test.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/server/routes/common.go`
- Modify: `backend/internal/server/router.go`
- Modify: `backend/cmd/server/wire.go`
- Regenerate: `backend/cmd/server/wire_gen.go`

1. Add failing configuration tests for disabled defaults and enabled-without-secret rejection.
2. Add failing handler tests for missing, malformed, stale, and invalid signatures plus a valid signed response.
3. Implement constant-time HMAC verification, no-store response headers, and disabled-route behavior.
4. Wire the service and handler, regenerate Wire output, and run targeted tests.

## Task 5: Add dynamic admin controls

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/admin/GroupsView.vue`
- Modify: relevant locale files under `frontend/src/locales/`
- Test: `frontend/src/views/admin/__tests__/groupsProviderPricing.spec.ts`

1. Add failing unit tests for normalizing the Hvoy group name and model list payload.
2. Add create/edit controls visible for OpenAI groups: publish switch, stable `gptNN` identifier, and explicit model list.
3. Include the fields in create/update payloads and reset/edit hydration.
4. Run frontend unit tests, typecheck, and production build.

## Task 6: Full verification and governance closeout

**Files:**
- Create: `docs/governance/runs/hvoy-provider-pricing.md`

1. Run `gofmt` on touched Go files.
2. Run `env -u OPENAI_API_KEY go test ./...` and backend build from `backend/`.
3. Run frontend tests, typecheck, and build.
4. Record commands, results, exclusions, and rollback boundary in the run log.
5. Commit the implementation in coherent slices.

## Task 7: Land and deploy to 43 V2

1. Confirm `dev/omni` has not moved and land only the new commits without absorbing unrelated dirty files.
2. Push `dev/omni`.
3. Read the live V2 compose/runtime state and record the current image for rollback.
4. Build and install a versioned V2 image, enable the provider-pricing API with a server-generated production secret, and restart only the V2 app.
5. Configure internal group id 10 as `gpt01` with the approved GPT model list.
6. Verify `/health`, unsigned 401, stale 401, bad signature 401, valid signed schema 1.1 response, group name `gpt01`, final price arithmetic, and unchanged V1 containers.
7. Record the deployed image, live evidence, and rollback command in the run log.

