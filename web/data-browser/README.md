# Toktik Data Browser

Internal data browsing UI for Toktik market datasets.

```bash
npm install
npm run dev
```

By default the Vite dev server proxies `/api` and `/ready` to `http://localhost:9010`. Override with `VITE_PROXY_TARGET` when the API server runs elsewhere.

The UI stores an optional `X-API-Key` in local storage for internal deployments that enable API key auth.
