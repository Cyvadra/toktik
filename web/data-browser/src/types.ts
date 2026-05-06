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
  close?: number;
  mark_close?: number;
  last_close?: number;
  underlying_close?: number;
  volume?: number;
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
