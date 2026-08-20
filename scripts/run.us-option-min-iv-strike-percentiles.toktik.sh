#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
api_key_file="$script_dir/../toktik-api-key"
api_key=${TOKTIK_API_KEY:-}
if [[ -z "$api_key" && -f "$api_key_file" ]]; then
  IFS= read -r api_key < "$api_key_file" || [[ -n "$api_key" ]]
fi
if [[ -z "$api_key" ]]; then
  printf 'TOKTIK_API_KEY or toktik-api-key is required\n' >&2
  exit 1
fi

# Every value can be overridden for quick strategy experiments. JSON variables
# are validated below so malformed input fails before a backtest is submitted.
dsl_file=${TOKTIK_DSL_FILE:-"$script_dir/../pkg/dsl/scripts/strategies/us-option-min-iv-strike-percentiles.toktik"}
market=${TOKTIK_MARKET:-us}
instrument=${TOKTIK_INSTRUMENT:-mixed}
asset=${TOKTIK_ASSET:-SPY}
symbols_json=${TOKTIK_SYMBOLS_JSON:-'["SPY"]'}
interval=${TOKTIK_INTERVAL:-1d}
from_date=${TOKTIK_FROM:-2023-01-01}
to_date=${TOKTIK_TO:-2025-12-31}
capital=${TOKTIK_CAPITAL:-100000}
dsl_params_json=${TOKTIK_DSL_PARAMS_JSON:-'{"Symbol":"SPY"}'}
dsl_profile_json=${TOKTIK_DSL_PROFILE_JSON:-'{"uses_options":true,"regular_trade":"material"}'}

[[ -f "$dsl_file" ]] || { printf 'DSL file not found: %s\n' "$dsl_file" >&2; exit 1; }
jq -e . >/dev/null <<< "$symbols_json"
jq -e . >/dev/null <<< "$dsl_params_json"
jq -e . >/dev/null <<< "$dsl_profile_json"

payload=$(jq -n \
  --rawfile dsl "$dsl_file" \
  --arg market "$market" \
  --arg instrument "$instrument" \
  --arg asset "$asset" \
  --arg interval "$interval" \
  --arg from "$from_date" \
  --arg to "$to_date" \
  --argjson symbols "$symbols_json" \
  --argjson capital "$capital" \
  --argjson dsl_params "$dsl_params_json" \
  --argjson dsl_profile "$dsl_profile_json" \
  '{
    market: $market,
    instrument: $instrument,
    asset: $asset,
    symbols: $symbols,
    interval: $interval,
    from: $from,
    to: $to,
    capital: $capital,
    dsl: $dsl,
    dsl_params: $dsl_params,
    dsl_profile: $dsl_profile
  }')

api_base_url=${TOKTIK_API_BASE_URL:-http://127.0.0.1:9010}
api_base_url=${api_base_url%/}

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
