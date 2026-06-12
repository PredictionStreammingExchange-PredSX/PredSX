# PredSX — Rollback Procedure

> Step-by-step guide for rolling back to a previous deployment when a new deploy breaks something.
> This procedure relies on git-hash image tags created by `build-and-ship.bat`.

---

## How Image Tagging Works

Every time `build-and-ship.bat` runs, it tags each of the 5 images with **two tags**:

```
predsx-api:latest          ← always points to the most recent build
predsx-api:cc27bce         ← permanent tag for this specific commit
```

The short git hash (`cc27bce`) is printed at the end of every build. The hash images remain on the VPS until you explicitly remove them — they are your rollback snapshots.

---

## Before You Deploy — Save the Current Hash

Before pushing a new deploy, note the hash of the currently running images:

```bash
# On VPS — see which hash tag is currently in use
docker images | grep predsx
```

Output example:
```
predsx-api        latest    sha256:abc   10 minutes ago   25MB
predsx-api        cc27bce   sha256:abc   10 minutes ago   25MB
predsx-api        e71dc92   sha256:xyz   2 days ago       24MB
```

The older hash (`e71dc92` here) is your rollback target if the new deploy breaks something.

You can also check `git log` locally to correlate:
```powershell
git log --oneline -5
```

---

## Rollback Procedure

### Step 1 — SSH into the VPS

```powershell
ssh -i $env:USERPROFILE\.ssh\your_ssh_key user@YOUR_VPS_IP
cd ~/predsx/package
```

### Step 2 — Stop the running stack

```bash
docker compose down
```

This stops and removes containers but **does not delete volumes**. All ClickHouse data, Kafka offsets, and Postgres state are preserved.

### Step 3 — Rewrite image tags in `docker-compose.yml`

Replace `:latest` with the old git hash in the compose file. Substitute `OLDHASH` with the actual hash (e.g. `e71dc92`):

```bash
sed -i 's/:latest/:OLDHASH/g' docker-compose.yml
```

Verify the change:
```bash
grep "predsx-" docker-compose.yml
```

Expected output (all 5 services now pointing at the old hash):
```yaml
image: predsx-api:e71dc92
image: predsx-discovery:e71dc92
image: predsx-ingestion:e71dc92
image: predsx-processor:e71dc92
image: predsx-storage:e71dc92
```

### Step 4 — Start the stack on the old images

```bash
docker compose up -d
```

Docker will use the hash-tagged images that are already loaded — no re-transfer needed.

### Step 5 — Verify rollback succeeded

```bash
docker ps --format "table {{.Names}}\t{{.Status}}"
curl -s http://localhost:8088/health
```

All containers should show `(healthy)` and the health check should return `"status": "ok"`.

---

## Restore to Latest After a Fix

Once you've fixed the issue on your dev machine, rebuild and redeploy:

```bash
# On local machine — build with the fix
.\build-and-ship.bat
scp -i $env:USERPROFILE\.ssh\your_ssh_key -r .\deployments\package\ user@YOUR_VPS_IP:~/predsx/
```

On VPS — restore `:latest` tags and bring up the new images:

```bash
# Undo the hash pin
sed -i 's/:OLDHASH/:latest/g' docker-compose.yml

# Load the new tars
ls *.tar | xargs -I {} docker load -i {}

# Restart
docker compose down
docker compose up -d
```

---

## Rolling Back a Single Service

If only one service is broken (e.g. only `hub-api` is misbehaving after a deploy):

```bash
# Stop just the broken service
docker compose stop hub-api

# Point only hub-api at the old hash
sed -i 's/predsx-api:latest/predsx-api:OLDHASH/' docker-compose.yml

# Recreate only hub-api from old image
docker compose up -d --no-deps hub-api
```

All other services continue running uninterrupted.

---

## Checking Which Images Are Available on the VPS

```bash
docker images | grep predsx | sort
```

Images are stored on the VPS until you run `docker image rm` or `docker image prune`. Old hash images accumulate over time — prune them periodically to save disk space:

```bash
# Remove images not referenced by any container (keeps currently running ones safe)
docker image prune -a --filter "until=168h"   # removes images older than 7 days
```

---

## Important: Never Use `docker compose down -v`

The `-v` flag deletes all named Docker volumes. This permanently destroys:
- All ClickHouse trade and candle history
- Kafka offsets (consumers will reprocess from the beginning)
- Postgres backfill state

Always use plain `docker compose down` for rollbacks and restarts.
