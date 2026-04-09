# Polygon Market Data API

This document describes the external Polygon-backed market data endpoints for US stocks and US options.

## When to use these endpoints

- Use the Polygon endpoints for realtime or near-realtime US stock and US option data.
- Use these endpoints when you need direct market-data reads rather than delayed or warehouse-oriented analytics endpoints.
- For intraday polling, prefer the realtime snapshot, quote, trade, and chain endpoints.

## Endpoint summary

Base path: `/api/v1/polygon`

Replace `http://localhost:9010` in the examples below with your assigned API domain.

Stocks:

- `/stocks/snapshot`
- `/stocks/aggregates`

Options:

- `/options/contract`
- `/options/chain`
- `/options/aggregates`

## Realtime queries

Realtime stock snapshot:

```bash
curl "http://localhost:9010/api/v1/polygon/stocks/snapshot?symbol=AAPL"
```

Realtime option chain snapshot:

```bash
curl "http://localhost:9010/api/v1/polygon/options/chain?underlying=SPY&expiration_date_gte=2026-04-09&expiration_date_lte=2026-05-16&contract_type=call&limit=50"
```

## Historical queries

Historical stock minute bars:

```bash
curl "http://localhost:9010/api/v1/polygon/stocks/aggregates?ticker=NVDA&multiplier=1&timespan=minute&from=2026-04-02&to=2026-04-08&adjusted=true&limit=50000"
```

Historical option minute bars:

```bash
curl "http://localhost:9010/api/v1/polygon/options/aggregates?ticker=O:SPY260417C00580000&multiplier=1&timespan=minute&from=2026-04-08&to=2026-04-09&limit=50000"
```

## Python examples

Realtime stock snapshot:

```python
import requests

base_url = "http://localhost:9010/api/v1/polygon"
resp = requests.get(f"{base_url}/stocks/snapshot", params={"symbol": "AAPL"}, timeout=10)
resp.raise_for_status()
data = resp.json()["data"]
print(data["ticker"], data.get("lastTrade", {}))
```

Realtime option chain:

```python
import requests

base_url = "http://localhost:9010/api/v1/polygon"
params = {
    "underlying": "SPY",
    "expiration_date_gte": "2026-04-09",
    "expiration_date_lte": "2026-05-16",
    "contract_type": "call",
    "limit": 25,
}
resp = requests.get(f"{base_url}/options/chain", params=params, timeout=15)
resp.raise_for_status()
contracts = resp.json()["data"]
print(len(contracts), contracts[0]["contract"]["ticker"])
```

Historical aggregates:

```python
import requests

base_url = "http://localhost:9010/api/v1/polygon"
params = {
    "ticker": "AAPL",
    "multiplier": 1,
    "timespan": "minute",
    "from": "2026-04-08",
    "to": "2026-04-09",
    "adjusted": True,
    "limit": 5000,
}
resp = requests.get(f"{base_url}/stocks/aggregates", params=params, timeout=20)
resp.raise_for_status()
bars = resp.json()["data"]
print(bars[0]["timestamp"], bars[0]["close"])
```

## Notes

- These endpoints are intended primarily for realtime and near-realtime access.
- Historical reads are supported, but the main recommended use case is realtime snapshot, chain, quote, and trade queries.
- Some responses may be cached briefly by the platform to reduce repeated upstream requests. This is transparent to clients.
