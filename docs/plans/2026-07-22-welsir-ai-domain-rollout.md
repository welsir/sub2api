# Welsir AI Domain Rollout Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Publish the isolated Omni V2 service as Welsir AI at `ai.welsir.com` without changing or restarting the Legacy service.

**Architecture:** Keep V2 bound to `127.0.0.1:18084` and terminate public HTTP/TLS in the existing host Nginx. Use a dedicated `ai.welsir.com` server block and certificate; leave `omni.welsir.com`, the storefront, Legacy containers and Legacy data untouched.

**Tech Stack:** Nginx, Certbot, Docker Compose, Sub2API, PostgreSQL, `tml-ssh-ops`

---

### Task 1: Record the pre-publish baseline

**Files:**
- Read: `/etc/nginx/conf.d/omni-welsir-sub2api.conf`
- Read: `/data/sub2api-v2/docker-compose.yml`

**Step 1: Record container health and restart counts**

Run through `tml-ssh-ops`:

```bash
sudo docker inspect --format '{{.Name}}|{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}|restarts={{.RestartCount}}' \
  sub2api-v2-app sub2api-v2-postgres sub2api-v2-redis \
  sub2api-preview-sub2api-1 sub2api-preview-sub2api-postgres-1 sub2api-preview-sub2api-redis-1
```

Expected: all six containers are running and healthy; Legacy restart counts are unchanged.

**Step 2: Verify private V2 health**

```bash
curl -fsS --max-time 5 http://127.0.0.1:18084/health
```

Expected: `{"status":"ok"}`.

**Step 3: Record current CPU, memory and disk state**

```bash
free -m
df -h /data /mnt
uptime
```

Expected: V2 remains within existing resource limits and `/data` has sufficient space.

### Task 2: Apply the Welsir AI public identity

**Files:**
- Modify: `deploy/omni-v2/nginx-http.conf`
- Modify remotely through V2 PostgreSQL: `public.settings`

**Step 1: Set the Nginx template hostname**

Replace `__V2_DOMAIN__` with `ai.welsir.com` in the V2-only Nginx template.

**Step 2: Update only V2 public settings**

Set:

```text
site_name=Welsir AI
site_subtitle=多模型 API 中转服务
api_base_url=https://ai.welsir.com
frontend_url=https://ai.welsir.com
```

Do not update Legacy settings or copy Legacy users, balances, keys, orders or usage.

**Step 3: Restart only the V2 app**

```bash
sudo docker restart sub2api-v2-app
```

Expected: V2 returns healthy and the page title contains `Welsir AI`; Legacy restart counts remain unchanged.

### Task 3: Prepare the HTTP virtual host

**Files:**
- Create: `/etc/nginx/sites-available/welsir-ai`
- Create: `/etc/nginx/sites-enabled/welsir-ai`
- Preserve: `/etc/nginx/conf.d/omni-welsir-sub2api.conf`

**Step 1: Install the dedicated HTTP server block**

Use `deploy/omni-v2/nginx-http.conf`, proxying the complete site to `127.0.0.1:18084`.

**Step 2: Validate before reload**

```bash
sudo nginx -t
```

Expected: syntax and configuration tests succeed.

**Step 3: Reload Nginx without restarting it**

```bash
sudo systemctl reload nginx
```

**Step 4: Test by Host header before DNS**

```bash
curl -fsS -H 'Host: ai.welsir.com' http://127.0.0.1/health
curl -fsS -H 'Host: ai.welsir.com' http://127.0.0.1/ | grep -o '<title>[^<]*</title>'
```

Expected: V2 health is OK and the page title is Welsir AI.

### Task 4: Add DNS and issue TLS

**Files:**
- Modify externally: DNS A record for `ai.welsir.com`
- Create through Certbot: `/etc/letsencrypt/live/ai.welsir.com/`
- Modify through Certbot: `/etc/nginx/sites-available/welsir-ai`

**Step 1: Add the DNS record**

```text
Type: A
Name: ai
Value: 43.199.92.179
```

Expected: public DNS resolvers return `43.199.92.179`.

**Step 2: Issue the certificate**

Run Certbot only after DNS resolves:

```bash
sudo certbot --nginx -d ai.welsir.com
```

Expected: the certificate is issued and Nginx redirects HTTP to HTTPS.

**Step 3: Revalidate Nginx and certificate renewal**

```bash
sudo nginx -t
sudo certbot renew --dry-run
```

Expected: both commands succeed.

### Task 5: Complete public acceptance

**Files:**
- Record remotely: `/data/sub2api-v2/ops/post-launch/`

**Step 1: Verify the public page and health endpoint**

```bash
curl -fsS https://ai.welsir.com/health
curl -fsS https://ai.welsir.com/ | grep -o '<title>[^<]*</title>'
```

Expected: health is OK and the title is Welsir AI.

**Step 2: Verify the API contract with a V2-only key**

Run the existing private acceptance flow against `https://ai.welsir.com` and verify:

- `/v1/models`
- non-stream `/v1/responses`
- stream `/v1/responses`
- tool call
- V2-only billing records

**Step 3: Verify Legacy isolation**

Compare Legacy users, API keys, usage rows, health and restart counts with the Task 1 baseline.

Expected: no Legacy state changed.

**Step 4: Record rollback instructions**

Rollback disables only `/etc/nginx/sites-enabled/welsir-ai`, validates Nginx and reloads it. V2 may remain private on port `18084`; Legacy is not part of rollback.
