# PredSX — CLI Tool Reference

> The `cmd/predsx` CLI lets you query live market data from the terminal without a browser.
> It talks to the PredSX API (`hub-api`) over HTTP and renders output as a formatted table or JSON.

---

## Installation

The CLI is built as part of the normal Docker build, but you can also build and run it locally:

```powershell
# From the PredSX root directory
cd predsx
go run ./cmd/predsx --help
```

Or build a binary:
```powershell
go build -o predsx.exe ./cmd/predsx
```

---

## Global Flags

These flags apply to every subcommand:

| Flag | Default | Description |
|------|---------|-------------|
| `--api-url` | `http://localhost:8080` | Base URL of the PredSX API. Override to point at your VPS. |
| `--json` | `false` | Output raw JSON instead of a formatted table. Useful for piping to `jq`. |

**Point at your VPS:**
```bash
predsx --api-url http://YOUR_VPS_IP:8088 markets
```

> **Note:** The default port in the CLI is `8080`. The VPS API runs on `8088`. Always pass `--api-url` when querying the VPS.

---

## Subcommands

### `markets` — List active markets

Fetches the first page of markets from `/v1/markets` and displays them in a table.

```bash
predsx markets
predsx --api-url http://YOUR_VPS_IP:8088 markets
```

**Table output:**
```
  ID          TITLE
  558935      Will Cristiano Ronaldo win Ballon d'Or 2025?
  821047      Will Bitcoin reach $200k before January 2026?
  ...
```

**JSON output:**
```bash
predsx --api-url http://YOUR_VPS_IP:8088 --json markets
```
```json
[
  {
    "market_id": "558935",
    "title": "Will Cristiano Ronaldo win Ballon d'Or 2025?",
    "status": "ACTIVE",
    ...
  }
]
```

---

### `trades <market_id>` — Latest trades for a market

Fetches recent trades from `/v1/markets/{id}/trades` for the given market ID.

```bash
predsx trades 558935
predsx --api-url http://YOUR_VPS_IP:8088 trades 558935
```

**Table output:**
```
  ID                  SIDE    PRICE   SIZE
  0xf1e2d3c4...       BUY     0.632   500.00
  0xa1b2c3d4...       SELL    0.628   1200.00
```

**JSON output:**
```bash
predsx --api-url http://YOUR_VPS_IP:8088 --json trades 558935
```

Returns `Error: ...` if the market ID does not exist or no trades are in ClickHouse yet.

---

### `orderbook <market_id>` — Orderbook snapshot

Fetches the current orderbook from `/v1/markets/{id}/orderbook` for the given market ID.

```bash
predsx orderbook 558935
predsx --api-url http://YOUR_VPS_IP:8088 orderbook 558935
```

**Output:** Displays the best bid, best ask, and top levels of the bids/asks ladder.

Returns `Error: orderbook not found` if ingestion hasn't cached a snapshot for this market yet. Wait a few seconds after boot and retry.

---

### `price <market_id>` — Current live price

Fetches the current live price from `/v1/markets/{id}/price` for the given market ID.

```bash
predsx price 558935
predsx --api-url http://YOUR_VPS_IP:8088 price 558935
```

**Table output:**
```
  MARKET ID   PRICE
  558935      0.63
```

**JSON output:**
```bash
predsx --api-url http://YOUR_VPS_IP:8088 --json price 558935
```
```json
{
  "market_id": "558935",
  "price": 0.63,
  "mid_price": 0.635,
  "best_bid": 0.63,
  "best_ask": 0.64,
  "spread": 0.01,
  "volume_24h": 12480000.0,
  "timestamp": 1749599998000
}
```

Returns `Error: price not found` if the processor hasn't received a live trade or orderbook event for this market yet.

---

## Common Usage Patterns

**Quick market scan — get IDs for further queries:**
```bash
predsx --api-url http://YOUR_VPS_IP:8088 --json markets | python3 -c \
  "import sys,json; [print(m['market_id'], m['title'][:60]) for m in json.load(sys.stdin)['data']]"
```

**Check price and trades for a specific market:**
```bash
ID=558935
predsx --api-url http://YOUR_VPS_IP:8088 price $ID
predsx --api-url http://YOUR_VPS_IP:8088 trades $ID
```

**Pipe JSON to `jq` for filtering:**
```bash
predsx --api-url http://YOUR_VPS_IP:8088 --json markets | jq '.data[] | select(.status == "ACTIVE") | .title'
```

---

## Limitations

- `markets` subcommand fetches only the first page (default 50 results). There is no `--cursor` or `--limit` flag in the current implementation.
- `orderbook` output does not render a full bid/ask ladder table — it outputs the raw response. Use `--json` and `jq` for structured inspection.
- No `search`, `candles`, `signals`, or `stats` subcommands yet — use `curl` or the browser for those endpoints.
