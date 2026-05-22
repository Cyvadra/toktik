export type BrowserDataset = {
  name: string;
  market: string;
  relation: string;
  time_field?: string;
  symbol_field?: string;
  underlying_field?: string;
  fields?: string[];
  checks?: string[];
};

export type BrowserPresetResponse = {
  datasets: BrowserDataset[];
};

export type BrowserColumn = {
  name: string;
  type: string;
  position: number;
  default_kind?: string;
  default_expression?: string;
  comment?: string;
  codec_expression?: string;
  is_nullable: boolean;
};

export type BrowserSchemaResponse = {
  dataset: BrowserDataset;
  columns: BrowserColumn[];
};

export type BrowserPreviewResponse = {
  dataset: BrowserDataset;
  columns: string[];
  data: Array<Record<string, unknown>>;
};

export type BrowserCoverageResponse = {
  dataset: BrowserDataset;
  row_count: number;
  first_timestamp?: string;
  last_timestamp?: string;
  daily?: Array<{ date: string; row_count: number }>;
};

export type BrowserFieldProfileResponse = {
  dataset: BrowserDataset;
  field: string;
  type: string;
  row_count: number;
  null_count: number;
  zero_count?: number;
  empty_count?: number;
  distinct_count: number;
  min?: unknown;
  max?: unknown;
};

export type BrowserValidCountResponse = {
  dataset: BrowserDataset;
  check: string;
  row_count: number;
  valid_count: number;
  invalid_count: number;
};

export type BrowserValueListResponse = {
  dataset: BrowserDataset;
  fields: Array<{
    field: string;
    values: Array<{
      value: string;
      row_count: number;
      last_timestamp?: string;
    }>;
  }>;
  cached: boolean;
};

export type DatasetCatalogResponse = {
  summary: {
    total: number;
    ready: number;
    stale: number;
    missing: number;
    empty: number;
  };
  datasets: Array<{
    name: string;
    market: string;
    relation: string;
    status: string;
    freshness: string;
    row_count: number;
    last_timestamp?: string;
  }>;
};

export type BarRow = {
  timestamp: string;
  symbol?: string;
  symbol_id?: string | number;
  base_asset?: string;
  underlying?: string;
  option_type?: string;
  expiration?: string;
  strike?: number;
  open?: number;
  high?: number;
  low?: number;
  close?: number;
  mark_open?: number;
  mark_high?: number;
  mark_low?: number;
  mark_close?: number;
  last_open?: number;
  last_high?: number;
  last_low?: number;
  last_close?: number;
  bid_open?: number;
  bid_high?: number;
  bid_low?: number;
  bid_close?: number;
  ask_open?: number;
  ask_high?: number;
  ask_low?: number;
  ask_close?: number;
  underlying_price_open?: number;
  underlying_price_high?: number;
  underlying_price_low?: number;
  underlying_price_close?: number;
  underlying_close?: number;
  volume?: number;
  transactions?: number;
  tick_count?: number;
  open_interest?: number;
  implied_volatility?: number;
  delta?: number;
  gamma?: number;
  vega?: number;
  theta?: number;
  rho?: number;
  [key: string]: unknown;
};

export type BarResponse = {
  data: BarRow[];
  next_cursor?: string;
};

export type ChainResponse = {
  data: Array<{
    timestamp: string;
    underlying?: string;
    base_asset?: string;
    contracts: Array<Record<string, unknown>>;
  }>;
  next_cursor?: string;
};

export type CryptoOptionSymbolResponse = {
  data: Array<{
    symbol_id: number;
    symbol: string;
    base_asset: string;
    option_type: string;
    strike_price: number;
    expiration: string;
    underlying_index: string;
  }>;
  next_cursor?: string;
};

export type MarketSymbolRow = {
  symbol?: string;
  symbol_id?: string | number;
  underlying?: string;
  base_asset?: string;
  root?: string;
  option_type?: string;
  expiration?: string;
  strike?: number;
  strike_price?: number;
  profile?: {
    sector?: string;
    industry?: string;
  };
  [key: string]: unknown;
};

export type MarketSymbolResponse = {
  data: MarketSymbolRow[];
  next_cursor?: string;
};

export type FundamentalFactorCatalogEntry = {
  market: string;
  factor_code: string;
  display_name: string;
  description?: string;
  value_type: string;
  unit?: string;
  preferred_frequency: string;
  fill_policy: string;
  fill_max_days?: number;
  point_in_time: boolean;
  source?: string;
  active: boolean;
  sla_hours?: number;
  metadata?: string;
  updated_at: string;
};

export type FundamentalFactorCatalogResponse = {
  data: FundamentalFactorCatalogEntry[];
};

export type FundamentalSeriesPoint = {
  event_ts: string;
  known_at: string;
  value: number;
  source?: string;
  revision?: number;
  filled?: boolean;
};

export type FundamentalSeriesResponse = {
  market: string;
  symbol: string;
  factor: string;
  mode: string;
  as_of: string;
  fill_policy?: string;
  data: FundamentalSeriesPoint[];
};
