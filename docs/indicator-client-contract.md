# Indicator Client Contract

This document defines the recommended client integration contract for technical indicators served by `9010`.

## Goals

- Keep all preset technical indicators computed on the backend against the requested K-line window.
- Let the client discover built-in presets dynamically instead of hardcoding series keys.
- Let the client submit custom expressions without adding a new backend endpoint for each indicator.

## Endpoints

### Preset catalog

- Method: `GET`
- Path: `/api/v1/indicators/presets`
- Purpose: Returns the built-in preset groups and the series each preset expands to.

Example response shape:

```json
{
  "presets": [
    {
      "id": "classic-volatility",
      "name": "Classic Volatility",
      "description": "ATR and Bollinger Bands with common default parameters.",
      "indicators": [
        {
          "key": "atr_14",
          "expression": "ta.atr(14)"
        },
        {
          "key": "bb_upper_20_2",
          "expression": "ta.bb_upper(close,20,2.0)"
        }
      ]
    }
  ]
}
```

Client usage:

- Render preset selector from `presets[]`.
- Use `id` as the stable submit value.
- Use `indicators[].key` as the display label seed or as the default chart series key.

### Series query

- Method: `POST`
- Path: `/api/v1/indicators/series`
- Purpose: Compute preset and/or custom indicator series on top of the requested market bars.

Request shape:

```json
{
  "market": "us-stocks",
  "symbol": "AAPL",
  "interval": "1h",
  "from": "2024-01-01",
  "to": "2024-02-01",
  "presets": ["classic-moving-averages", "classic-momentum"],
  "indicators": ["ta.percentrank(ta.rsi(close,14),20)"],
  "precision": 2
}
```

Response shape:

```json
{
  "market": "us-stocks",
  "symbol": "AAPL",
  "interval": "1h",
  "timestamps": ["2024-01-01T10:00:00Z"],
  "series": {
    "sma_20": [182.34],
    "rsi_14": [54.22],
    "ta.percentrank(ta.rsi(close,14),20)": [80.0]
  }
}
```

Key behavior:

- All output is computed from the bars selected by `market`, `symbol`, `interval`, `from`, and `to`.
- `presets[]` expands server-side into named plot series such as `rsi_14`, `atr_14`, or `bb_upper_20_2`.
- `indicators[]` is for lightweight custom expressions. The response key defaults to the original expression string.
- `dsl` remains available for advanced use cases that need variables, conditional logic, or custom plot titles.

## Recommended UI model

### Mode 1: Preset-first

Use this for common workflows such as RSI, ATR, moving averages, and Bollinger Bands.

- On page load, fetch `/api/v1/indicators/presets`.
- Render preset cards or grouped checkboxes.
- Store selected preset IDs in client state.
- Submit selected IDs via `presets[]`.

### Mode 2: Expression builder

Use this for user-defined formulas that still fit one-line expressions.

- Let users pick a function, source, and numeric parameters.
- Compile that builder state into DSL expressions in `indicators[]`.
- Submit expressions together with any selected presets.

Suggested builder constraints:

- Only allow functions documented in `docs/dsl.md`.
- Validate parameter count and numeric range on the client before submit.
- Preserve the raw expression for replay and saved views.

### Mode 3: Advanced DSL

Use this for power users.

- Show a code editor backed by the DSL syntax.
- Submit content via `dsl`.
- Allow `dsl` to coexist with `presets[]` when users want to start from defaults and add extra plots.

## Recommended client-side validation

- Require `market`, `symbol`, `interval`, `from`, and `to`.
- Require at least one of `presets[]`, `indicators[]`, or `dsl`.
- If using `indicators[]`, reject empty strings before submit.
- If using saved views, persist both the selected preset IDs and the custom expressions.

## Suggested saved-view schema

```json
{
  "market": "crypto-spot",
  "symbol": "BTCUSDT",
  "interval": "4h",
  "selectedPresetIds": ["classic-volatility"],
  "customIndicators": ["ta.ema(close,34)", "ta.change(volume,5)"],
  "precision": 2
}
```

This keeps the backend contract stable while letting the client evolve the UX independently.