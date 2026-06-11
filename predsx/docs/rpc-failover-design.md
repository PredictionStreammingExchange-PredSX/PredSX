# Polygon RPC Failover & Rotation Design

## Why this exists

The on-chain indexer (`services/ingestion`, `startIndexerComponent` /
`runIndexerCycle` in [main.go](../services/ingestion/main.go)) reads CTF trade
events from the Polygon chain via `ethclient`. It originally pointed at a
single RPC provider (Alchemy/Infura-style). That's a single point of failure:
if that provider rate-limits us, disables the API key/tenant, or has an
outage, the indexer stalls completely and on-chain trade ingestion stops.

Free public RPC endpoints are individually unreliable (rate limits, random
520s, auth walls that appear without notice), but **a pool of them, rotated
intelligently, is very reliable** — at any given moment at least one is
usually healthy. This doc describes the rotation/failover system that makes
that work, so the indexer effectively never gets knocked out.

## The endpoint pool

`defaultRPCPool` ([main.go:349](../services/ingestion/main.go#L349)) lists
free, no-API-key Polygon mainnet RPC endpoints:

```go
var defaultRPCPool = []string{
    "https://polygon-rpc.com",
    "https://rpc.ankr.com/polygon",
    "https://polygon.meowrpc.com",
    "https://1rpc.io/matic",
    "https://polygon-bor-rpc.publicnode.com",
    "https://polygon.drpc.org",
}
```

This is the default. It can be overridden without a code change via the
`POLYGON_RPC_URLS` environment variable — a comma-separated list
([main.go:58-64](../services/ingestion/main.go#L58)):

```
POLYGON_RPC_URLS=https://my-paid-endpoint.example.com,https://polygon-rpc.com,...
```

This means a paid provider (Alchemy, Infura, QuickNode, etc.) can be dropped
back into the pool at any time — it just becomes one more endpoint the
rotator can fall back away from if it ever gets rate-limited again.

## How rotation works — `rpcRotator`

`rpcRotator` ([main.go:361-395](../services/ingestion/main.go#L361)) is a
small thread-safe round-robin scheduler with a cooldown ("benching")
mechanism:

```go
type rpcRotator struct {
    mu      sync.Mutex
    urls    []string
    idx     int
    benched map[string]time.Time
}
```

- **`pick()`** returns the next endpoint in round-robin order, skipping any
  that are currently "benched" (cooling down after failing). If literally
  every endpoint is benched (e.g. a total network outage), it still returns
  the next one in line rather than blocking — so the loop keeps making
  progress and will recover the instant connectivity returns.
- **`bench(url, cooldown)`** marks an endpoint as unhealthy and pulls it out
  of rotation for the given duration (`2 minutes` in the current wiring).
  This gives rate limits time to reset and avoids hammering a dead endpoint.

## How failures are detected and handled

### Cycle-level: `startIndexerComponent`

([main.go:397-419](../services/ingestion/main.go#L397)) is the outer loop. On
each iteration it:

1. Asks the rotator for the next healthy endpoint (`rotator.pick()`).
2. Runs a full indexing cycle against it (`runIndexerCycle`).
3. If the cycle returns an error, it **benches that endpoint for 2 minutes**,
   logs a warning, sleeps 5 seconds (to avoid a hot error loop), and goes
   back to step 1 — picking a *different* endpoint.

```go
rpcURL := rotator.pick()
err := runIndexerCycle(ctx, svc, producer, rpcURL)
if err != nil {
    svc.Logger.Warn("indexer cycle failed, benching RPC and rotating", "rpc", rpcURL, "error", err)
    rotator.bench(rpcURL, 2*time.Minute)
    time.Sleep(5 * time.Second)
}
```

**Example log output during startup rotation:**
```
WARN  indexer cycle failed, benching RPC and rotating
      rpc=https://polygon-rpc.com
      error="dial https://polygon-rpc.com failed: connection refused (Alchemy tenant disabled)"

WARN  indexer cycle failed, benching RPC and rotating
      rpc=https://rpc.ankr.com/polygon
      error="rpc https://rpc.ankr.com/polygon unhealthy after 3 consecutive errors: 401 Unauthorized (API key required)"

WARN  indexer cycle failed, benching RPC and rotating
      rpc=https://polygon.meowrpc.com
      error="rpc https://polygon.meowrpc.com unhealthy after 3 consecutive errors: 520 (Cloudflare error)"

INFO  indexer cycle started
      rpc=https://polygon-bor-rpc.publicnode.com
      block=55482910
```

Because the loop never exits on error — it just rotates — a single bad
provider can never permanently stop ingestion. The `ctx.Done()` case is the
only clean exit, used for graceful shutdown.

### Connection-level: dial errors

If `ethclient.Dial(rpcURL)` fails outright (DNS failure, connection refused,
TLS error, etc.), `runIndexerCycle` returns immediately with
`fmt.Errorf("dial %s failed: %w", rpcURL, err)`. The endpoint name is
embedded in the error so logs clearly show *which* provider is failing — that
propagates straight up to the bench/rotate logic above.

**Example dial error log:**
```
WARN  indexer cycle failed, benching RPC and rotating
      rpc=https://polygon-rpc.com
      error="dial https://polygon-rpc.com failed: dial tcp: lookup polygon-rpc.com: no such host"
```

### Polling-level: `consecutiveErrors` threshold

Within a single cycle, the indexer polls the chain repeatedly
(`HeaderByNumber` to find the latest block, `FilterLogs` to fetch trade
events). A single transient error here (a momentary blip) shouldn't be
treated as "this endpoint is dead" — that would cause needless churn. So each
poll loop tracks a `consecutiveErrors` counter
([main.go:454-507](../services/ingestion/main.go#L454)):

```go
consecutiveErrors := 0
const maxConsecutiveErrors = 3 // bench this endpoint and rotate after repeated failures
```

- On success, the counter resets to `0`.
- On error, the counter increments. The cycle keeps retrying (via `continue`)
  as long as the count is below the threshold.
- Once `consecutiveErrors >= 3`, the cycle gives up and returns an error
  (`"rpc %s unhealthy after %d consecutive errors: %w"`), which triggers the
  outer loop's bench-and-rotate behavior.

**Example — transient error absorbed (counter < 3):**
```
DEBUG poll block=55482915 rpc=https://polygon-bor-rpc.publicnode.com
WARN  HeaderByNumber error (1/3): context deadline exceeded — retrying
DEBUG poll block=55482915 rpc=https://polygon-bor-rpc.publicnode.com
INFO  block=55482915 logs=0  ← recovered on next attempt, counter reset
```

**Example — sustained failure triggers rotation (counter reaches 3):**
```
WARN  HeaderByNumber error (1/3): read tcp: connection reset by peer
WARN  HeaderByNumber error (2/3): read tcp: connection reset by peer
WARN  HeaderByNumber error (3/3): read tcp: connection reset by peer
WARN  indexer cycle failed, benching RPC and rotating
      rpc=https://polygon-bor-rpc.publicnode.com
      error="rpc https://polygon-bor-rpc.publicnode.com unhealthy after 3 consecutive errors: ..."
INFO  indexer cycle started  rpc=https://polygon.drpc.org  block=55482916
```

## Putting it together — the failure → recovery flow

```
┌─────────────────────────────────────────────────────────────────┐
│ startIndexerComponent (outer loop)                               │
│                                                                   │
│   rpcURL := rotator.pick()  ───────────────┐                     │
│        │                                   │ round-robin,        │
│        ▼                                   │ skips benched URLs  │
│   runIndexerCycle(rpcURL)                  │                     │
│        │                                   │                     │
│   ┌────┴─────────────────────────┐         │                     │
│   │ dial fails? → return err     │         │                     │
│   │                               │         │                     │
│   │ poll loop:                    │         │                     │
│   │   success → reset counter     │         │                     │
│   │   error   → counter++         │         │                     │
│   │     counter >= 3 → return err │         │                     │
│   └────┬─────────────────────────┘         │                     │
│        │                                   │                     │
│        ▼ err != nil                        │                     │
│   rotator.bench(rpcURL, 2 min) ────────────┘                     │
│   sleep 5s, log warning, loop again                              │
└─────────────────────────────────────────────────────────────────┘
```

In effect:
- **Transient blips** (1-2 errors in a row) are absorbed silently — no
  rotation, no benching, just a retry on the same endpoint.
- **Sustained failure** on one endpoint (3+ consecutive poll errors, or a
  failed dial, or any other cycle-ending error) causes that endpoint to be
  benched for 2 minutes and the very next attempt to use a different,
  presumably-healthy endpoint.
- **Benched endpoints automatically return to rotation** after their cooldown
  expires, so a temporarily rate-limited provider gets retried once it's had
  time to recover — nothing is permanently blacklisted.
- **Total outages are survivable**: if every endpoint is benched, `pick()`
  still returns one (round-robin) so the loop keeps trying rather than
  deadlocking, and will pick up immediately once any endpoint becomes
  reachable again.

## Design philosophy: exactly one active RPC, rotation only on real failure

A core requirement for this system is: **at any given moment, the indexer runs
against exactly one RPC endpoint, paced under that endpoint's own rate limit —
it does not proactively cycle between providers.** Rotation is a *failure
response*, not a load-balancing strategy. This is intentional, and the design
above already enforces it end-to-end:

- `startIndexerComponent` picks **one** URL via `rotator.pick()` and hands it
  to `runIndexerCycle`, which then polls that single endpoint every 5 seconds,
  indefinitely — there is no second endpoint in play, no parallel requests, no
  alternation.
- The *only* path back to `rotator.pick()` is `runIndexerCycle` returning an
  error — which only happens after a failed dial or `maxConsecutiveErrors = 3`
  repeated polling failures (see "Polling-level" above). A healthy endpoint
  that is responding normally is never abandoned.
- Once the indexer lands on a working endpoint, it stays there. In practice
  we've observed `polygon-bor-rpc.publicnode.com` run error-free for many
  minutes (and counting) with zero rotation events — exactly "one RPC, under
  its limit, indefinitely."

**Steady-state log pattern (healthy endpoint, no rotation):**
```
INFO  block=55482910 logs=2 rpc=https://polygon-bor-rpc.publicnode.com
INFO  block=55482915 logs=0 rpc=https://polygon-bor-rpc.publicnode.com
INFO  block=55482920 logs=5 rpc=https://polygon-bor-rpc.publicnode.com
INFO  block=55482925 logs=0 rpc=https://polygon-bor-rpc.publicnode.com
# polling every 5s, same endpoint, no rotation
```

### Why startup looks like "rotation churn" (it isn't continuous rotation)

`rpcRotator.idx` is in-memory only and resets to `0` on every process restart.
Since `defaultRPCPool[0]` is `polygon-rpc.com` (permanently dead — disabled
Alchemy tenant), a fresh start has to walk past several broken/limited
endpoints (`polygon-rpc.com` → `rpc.ankr.com` → `polygon.meowrpc.com` →
`1rpc.io/matic`) before reaching a healthy one — roughly 80 seconds of
benching/rotating in a burst.

**Example — full startup rotation sequence:**
```
t=0s   WARN  benching polygon-rpc.com (dial failed, ~0s)
t=5s   WARN  benching rpc.ankr.com (401 unauthorized, ~15s to reach 3 errors)
t=20s  WARN  benching polygon.meowrpc.com (Cloudflare 520, ~15s)
t=35s  WARN  benching 1rpc.io/matic (rate limit, ~15s)
t=50s  INFO  stable on polygon-bor-rpc.publicnode.com — running normally
```

**This is a one-time discovery cost paid once per (re)start, not the system's
steady-state behavior.** After it lands on a good endpoint, rotation stops
completely until that endpoint actually breaks.

### Planned follow-up to minimize even that startup cost

To make the very first pick on every boot land on a proven-stable endpoint:

1. **Reorder `defaultRPCPool`** so `polygon-bor-rpc.publicnode.com` and
   `polygon.drpc.org` (the two endpoints observed to be reliable) come first,
   ahead of the known-dead/auth-walled ones.
2. **Drop `1rpc.io/matic` from the pool.** Its quota is a *daily request
   count* (10,000/day, reset at 00:00 UTC), not a per-second rate limit. Our
   indexer's load (~0.4 req/s average, up to ~34,500 req/day worst case under
   the current 40-block-chunk/5s-poll/1s-pacing setup) exceeds that daily cap
   by roughly 3x — meaning 1rpc.io will *always* be exhausted well before the
   day resets, guaranteeing a forced rotation no amount of pacing can prevent.

**Proposed updated pool:**
```go
var defaultRPCPool = []string{
    "https://polygon-bor-rpc.publicnode.com",  // proven stable
    "https://polygon.drpc.org",                // proven stable
    "https://polygon.meowrpc.com",             // intermittent
    "https://rpc.ankr.com/polygon",            // requires API key now
    // polygon-rpc.com removed (dead Alchemy tenant)
    // 1rpc.io/matic removed (10k/day cap — always exhausted)
}
```

With this order, a fresh start would land on a healthy endpoint in under 5 seconds instead of ~80 seconds.

## Real-world verification

This was observed live after deployment — the rotator correctly walked past
several endpoints with real-world problems and landed on a stable one:

| Endpoint | Observed issue | Outcome |
|---|---|---|
| `polygon-rpc.com` | Disabled Alchemy tenant (dial/auth error) | Benched, rotated past |
| `rpc.ankr.com/polygon` | Now requires an API key | Benched, rotated past |
| `polygon.meowrpc.com` | Intermittent Cloudflare 520 errors | Benched, rotated past |
| `1rpc.io/matic` | Healthy at the time | **Selected, ran error-free for 5+ minutes** |
| `polygon-bor-rpc.publicnode.com` | Healthy | **Selected in later runs, most stable** |

No manual restarts or config changes were needed — the system found a working
endpoint on its own and kept the trade-event pipeline flowing.

## Tuning knobs

| Setting | Location | Current value | Purpose |
|---|---|---|---|
| Endpoint pool | `defaultRPCPool` or `POLYGON_RPC_URLS` env var | 6 free public endpoints | Which providers are eligible |
| Consecutive-error threshold | `maxConsecutiveErrors` | `3` | How many polling errors in a row before an endpoint is considered unhealthy |
| Bench cooldown | `rotator.bench(rpcURL, 2*time.Minute)` | `2 minutes` | How long an unhealthy endpoint sits out before being retried |
| Retry backoff | `time.Sleep(5 * time.Second)` | `5 seconds` | Pause between cycle failure and the next attempt, to avoid a hot error loop |

**Override pool via environment variable (no rebuild needed):**
```bash
# docker-compose.yml
environment:
  - POLYGON_RPC_URLS=https://polygon-mainnet.g.alchemy.com/v2/YOUR_KEY,https://polygon-bor-rpc.publicnode.com
```

These are deliberately conservative defaults; they can be adjusted in
[main.go](../services/ingestion/main.go) if a particular pool of endpoints
needs faster/slower rotation.

## Related fix shipped alongside this: `price_history_1m` OHLC schema

While verifying the RPC changes, log monitoring surfaced an unrelated, active
bug: `flushMetrics` was inserting `(market_id, timestamp, trade_count, volume,
avg_price)` into **both** `market_metrics` and `price_history_1m`, but
`price_history_1m` is an OHLC candle table with schema `(market_id, timestamp,
open, high, low, close, volume, trade_count)`. Every flush to that table was
failing with `No such column avg_price`.

**Failing log (before fix):**
```
ERROR flushMetrics failed
      table=price_history_1m
      error="code: 16, message: No such column avg_price in table predsx.price_history_1m"
      dropped=500 events
```

**Fix:** `MetricBucket` now also tracks `Open`/`High`/`Low`/`Close`, computed in
`updateBucket`, and `flushMetrics` branches on the target table name to use
the correct column list and values for candle tables vs. simple metrics
tables.

**Correct insert after fix:**
```sql
-- market_metrics (simple metrics)
INSERT INTO market_metrics (market_id, timestamp, trade_count, volume, avg_price)
VALUES ('0x9f8e...', '2026-06-11 12:00:00', 48, 14820.50, 0.631)

-- price_history_1m (OHLC candles)
INSERT INTO price_history_1m (market_id, timestamp, open, high, low, close, volume, trade_count)
VALUES ('0x9f8e...', '2026-06-11 12:00:00', 0.610, 0.618, 0.608, 0.615, 14820.50, 48)
```

Verified post-fix: `price_history_1m` populates correctly with real
OHLC data and zero flush errors. This also unblocked the `/candles` endpoint
from returning `{"s":"no_data"}` on all queries.
