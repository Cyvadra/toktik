#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
api_key_file="$script_dir/toktik-api-key"
api_key=${TOKTIK_API_KEY:-}
if [[ -z "$api_key" && -f "$api_key_file" ]]; then
  IFS= read -r api_key < "$api_key_file"
fi
api_key=${api_key:?TOKTIK_API_KEY or scripts/toktik-api-key is required}

payload=$(jq -n \
  --rawfile dsl "$script_dir/../pkg/dsl/scripts/strategies/us-option-min-iv-strike-percentiles.toktik" \
  '{
    market: "us",
    instrument: "mixed",
    asset: "SPY",
    symbols: ["SPY"],
    interval: "1d",
    from: "2023-01-01",
    to: "2025-12-31",
    capital: 100000,
    dsl: $dsl,
    dsl_params: { Symbol: "SPY" },
    dsl_profile: {
      uses_options: true,
      regular_trade: "material"
    }
  }')

api_base_url=${TOKTIK_API_BASE_URL:-http://127.0.0.1:9020}

curl -sS -X POST \
  "$api_base_url/api/v1/backtests/validate" \
  -H "X-API-Key: $api_key" \
  -H "Content-Type: application/json" \
  --data "$payload" | jq

accepted=$(curl -sS -X POST \
  "$api_base_url/api/v1/backtests/runs" \
  -H "X-API-Key: $api_key" \
  -H "Content-Type: application/json" \
  --data "$payload")

printf '%s\n' "$accepted" | jq
run_id=$(printf '%s\n' "$accepted" | jq -r '.run_id')

curl -N \
  -H "X-API-Key: $api_key" \
  "$api_base_url/api/v1/backtests/runs/$run_id/events"

find "$script_dir/../reports/backtests/api/$run_id" -maxdepth 1 -type f -print
