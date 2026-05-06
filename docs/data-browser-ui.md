# Data Browser UI

Toktik Data Browser is an internal UI for browsing approved ClickHouse-backed market datasets without opening a raw SQL console.

## Backend

The existing API server now exposes browser endpoints under `/api/v1/browser`:

- `GET /api/v1/browser/presets`
- `GET /api/v1/browser/datasets/{dataset}/schema`
- `GET /api/v1/browser/datasets/{dataset}/preview`
- `GET /api/v1/browser/datasets/{dataset}/coverage`
- `GET /api/v1/browser/datasets/{dataset}/field-profile`
- `GET /api/v1/browser/datasets/{dataset}/valid-count`

The browser API only accepts server-approved dataset presets and columns. It does not expose custom SQL.

## Frontend

The frontend lives in `web/data-browser`.

```bash
make web-install
make web-dev
```

The Vite dev server proxies `/api` and `/ready` to `http://localhost:9010` by default. Use `VITE_PROXY_TARGET=http://host:port make web-dev` when the API server runs elsewhere.

For a production bundle:

```bash
make web-build
```

## Current Views

- Dataset schema and ClickHouse field metadata
- Row preview with symbol, underlying, time, column, and limit filters
- Time coverage with first/last timestamp and daily counts
- Field profile with null, zero, empty, distinct, min, and max stats
- Valid row counts using server-approved checks
- Simple market time series chart using existing market bars endpoints
- Option chain snapshot table for crypto and US options