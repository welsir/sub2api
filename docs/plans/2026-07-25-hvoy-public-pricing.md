# Hvoy Public Provider Pricing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow the 43 V2 Hvoy provider-pricing feed to be fetched without a shared HMAC secret while preserving secure-by-default behavior for every other environment.

**Architecture:** Add a `RequireHMAC` flag to the provider-pricing configuration and keep its default `true`. The HTTP adapter skips only its existing signature guard when the flag is explicitly `false`; pricing projection, group configuration, schema, caching, and routing remain unchanged.

**Tech Stack:** Go 1.26, Gin, Viper, Testify, Docker Compose, Nginx, PostgreSQL.

---

### Task 1: Add The Secure-By-Default Configuration Switch

**Files:**
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/internal/config/config.go`

**Step 1: Write the failing configuration tests**

Extend `TestLoadProviderPricingDefaultsDisabled` with:

```go
require.True(t, cfg.ProviderPricing.RequireHMAC)
```

Add:

```go
func TestLoadProviderPricingAllowsPublicFeedWithoutSecret(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("PROVIDER_PRICING_ENABLED", "true")
	t.Setenv("PROVIDER_PRICING_REQUIRE_HMAC", "false")

	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.ProviderPricing.Enabled)
	require.False(t, cfg.ProviderPricing.RequireHMAC)
	require.Empty(t, cfg.ProviderPricing.HMACSecret)
}
```

Update the strong-secret and enabled-config tests to set and assert `PROVIDER_PRICING_REQUIRE_HMAC=true` explicitly.

**Step 2: Run the tests to verify RED**

Run:

```bash
cd backend
go test ./internal/config -run TestLoadProviderPricing -count=1
```

Expected: FAIL because `ProviderPricingConfig.RequireHMAC` does not exist and public mode still requires a secret.

**Step 3: Implement the minimal configuration change**

Add to `ProviderPricingConfig`:

```go
RequireHMAC bool `mapstructure:"require_hmac"`
```

Add the default:

```go
viper.SetDefault("provider_pricing.require_hmac", true)
```

Change secret validation to:

```go
if c.ProviderPricing.Enabled && c.ProviderPricing.RequireHMAC {
	if len([]byte(strings.TrimSpace(c.ProviderPricing.HMACSecret))) < 32 {
		return fmt.Errorf("provider_pricing.hmac_secret must be at least 32 bytes when HMAC is required")
	}
}
```

Keep the site name and domain requirements under `ProviderPricing.Enabled`.

**Step 4: Run the tests to verify GREEN**

Run:

```bash
cd backend
go test ./internal/config -run TestLoadProviderPricing -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat(config): allow public provider pricing feed"
```

### Task 2: Make Authentication Conditional At The HTTP Adapter

**Files:**
- Modify: `backend/internal/handler/provider_pricing_handler_test.go`
- Modify: `backend/internal/handler/provider_pricing_handler.go`

**Step 1: Write the failing handler test**

Set `RequireHMAC: true` in `TestProviderPricingHandlerRequiresValidHMAC`, then add:

```go
func TestProviderPricingHandlerAllowsUnsignedPublicFeed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &providerPricingReaderStub{response: &service.ProviderPricingResponse{
		SchemaVersion: "1.1",
		Success:       true,
	}}
	h := NewProviderPricingHandler(reader, config.ProviderPricingConfig{
		Enabled:     true,
		RequireHMAC: false,
	})
	r := gin.New()
	r.GET("/api/provider/pricing", h.Get)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/provider/pricing", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
}
```

**Step 2: Run the test to verify RED**

Run:

```bash
cd backend
go test ./internal/handler -run TestProviderPricingHandler -count=1
```

Expected: FAIL because the unsigned public request returns `401`.

**Step 3: Implement the minimal handler change**

Change the authentication guard to:

```go
if h.cfg.RequireHMAC && !h.validSignature(c.GetHeader("X-Hvoy-Ts"), c.GetHeader("X-Hvoy-Sign")) {
```

Do not change the signature algorithm or response structure.

**Step 4: Run the tests to verify GREEN**

Run:

```bash
cd backend
go test ./internal/handler -run TestProviderPricingHandler -count=1
```

Expected: PASS for public and HMAC-enforced modes.

**Step 5: Commit**

```bash
git add backend/internal/handler/provider_pricing_handler.go backend/internal/handler/provider_pricing_handler_test.go
git commit -m "feat(api): permit unsigned Hvoy pricing access"
```

### Task 3: Run Repository Verification

**Files:**
- Verify only; no expected source changes.

**Step 1: Format touched Go files**

```bash
gofmt -w backend/internal/config/config.go backend/internal/config/config_test.go backend/internal/handler/provider_pricing_handler.go backend/internal/handler/provider_pricing_handler_test.go
```

**Step 2: Run the full backend suite**

```bash
cd backend
env -u OPENAI_API_KEY go test ./...
```

Expected: PASS.

**Step 3: Build the server with the embedded frontend**

```bash
cd backend
go build -tags embed ./cmd/server
```

Expected: exit code 0.

**Step 4: Check the diff**

```bash
git diff --check
git status --short
```

Expected: only the planned Hvoy public-access artifacts are present.

### Task 4: Record Verification And Land On `dev/omni`

**Files:**
- Modify: `docs/governance/runs/hvoy-provider-pricing.md`

**Step 1: Record the new public-access decision and local verification**

Document the default-secure switch, public 43 V2 target, test commands, deployment image, backups, and rollback boundary.

**Step 2: Commit the run-log update**

```bash
git add docs/governance/runs/hvoy-provider-pricing.md
git commit -m "docs: record public Hvoy pricing verification"
```

**Step 3: Fast-forward the existing `dev/omni` worktree without staging unrelated dirty files**

```bash
git -C /Users/welsir/.config/superpowers/worktrees/sub2api/omni-latest-selected-customs merge --ff-only codex/hvoy-provider-pricing
git -C /Users/welsir/.config/superpowers/worktrees/sub2api/omni-latest-selected-customs push origin dev/omni
```

Expected: `dev/omni` points to the public-access commits; existing unrelated working-tree files remain unstaged.

### Task 5: Deploy The Public Feed To 43 V2

**Files:**
- Remote modify: `/data/sub2api-v2/docker-compose.yml`
- Remote modify: `/data/sub2api-v2/.env`
- Remote preserve: current image and timestamped configuration backups

**Step 1: Recheck live V2 state**

Verify the active image, container health, `/mnt` capacity, compose keys, endpoint status, and staging-container availability through `tml-ssh-ops`.

**Step 2: Build and transfer the Linux amd64 server binary**

Build with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`, `-tags embed`, release metadata, and a new V2 image version. Transfer with strict host-key verification and compare SHA-256 hashes.

**Step 3: Derive the new image without deleting rollback artifacts**

Copy the binary into the retained staging container and commit a new image. Keep the HMAC-enabled image and staging container.

**Step 4: Back up and change the active V2 configuration**

- Back up the current compose and `.env` with a 2026-07-25 public-feed suffix.
- Add `PROVIDER_PRICING_REQUIRE_HMAC: "false"`.
- Remove `PROVIDER_PRICING_HMAC_SECRET` from the active compose and active `.env`.
- Validate `docker compose config --quiet` and prove the secret key name is absent without printing any secret value.

**Step 5: Recreate only the V2 app**

```bash
sudo -n docker compose --env-file /data/sub2api-v2/.env -f /data/sub2api-v2/docker-compose.yml up -d --no-deps app
```

Wait for `sub2api-v2-app` to become healthy.

**Step 6: Run live public acceptance**

Verify:

- unsigned public request: `200`
- request carrying Hvoy's default test signature: `200`
- response schema: `1.1`
- current row count: `10`
- unique external groups: `gpt01`
- public `/health`: `200`
- V2 app: healthy on the new image
- active container environment: no `PROVIDER_PRICING_HMAC_SECRET`
- V1, Preview, PostgreSQL, Redis, proxy, and gateway containers remain unchanged

**Step 7: Update the run log with exact deployment evidence**

Commit and push the final image, response, health, and rollback evidence to `dev/omni`.
