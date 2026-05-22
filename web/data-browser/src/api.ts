import type {
  BarResponse,
  BrowserCoverageResponse,
  BrowserFieldProfileResponse,
  BrowserPresetResponse,
  BrowserPreviewResponse,
  BrowserSchemaResponse,
  BrowserValidCountResponse,
  BrowserValueListResponse,
  ChainResponse,
  CryptoOptionSymbolResponse,
  DatasetCatalogResponse,
  FundamentalFactorCatalogResponse,
  FundamentalSeriesResponse,
  MarketSymbolResponse,
} from './types';

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? '';
const KEY_STORAGE = 'toktik.dataBrowser.apiKey';
const DB_CACHE_SESSION_PREFIX = 'toktik.dbCache.v1:session:';
const DB_CACHE_PERSISTENT_PREFIX = 'toktik.dbCache.v1:persistent:';
const DB_CACHE_DEFAULT_TTL_MS = 30_000;

type ApiGetOptions = {
  ttlMs?: number;
  cache?: boolean;
  persistent?: boolean;
  timeoutMs?: number;
};

type DbCacheEntry<T> = {
  expiresAt: number;
  value: T;
};

const dbCacheInflight = new Map<string, Promise<unknown>>();

export function getStoredApiKey() {
  return localStorage.getItem(KEY_STORAGE) ?? '';
}

export function setStoredApiKey(value: string) {
  const clean = value.trim();
  if (clean) {
    localStorage.setItem(KEY_STORAGE, clean);
  } else {
    localStorage.removeItem(KEY_STORAGE);
  }
  clearApiCache();
}

export function clearApiCache() {
  dbCacheInflight.clear();
  clearStorageCache(sessionStorage, DB_CACHE_SESSION_PREFIX);
  clearStorageCache(localStorage, DB_CACHE_PERSISTENT_PREFIX);
}

export async function apiGet<T>(path: string, params?: Record<string, string | number | undefined>, options: ApiGetOptions = {}) {
  const url = new URL(`${API_BASE}${path}`, window.location.origin);
  const sortedParams = Object.entries(params ?? {}).sort(([left], [right]) => left.localeCompare(right));
  for (const [key, value] of sortedParams) {
    if (value !== undefined && `${value}`.trim() !== '') {
      url.searchParams.set(key, `${value}`);
    }
  }
  const useCache = options.cache !== false;
  const ttlMs = options.ttlMs ?? DB_CACHE_DEFAULT_TTL_MS;
  const cacheStorage = options.persistent ? localStorage : sessionStorage;
  const cachePrefix = options.persistent ? DB_CACHE_PERSISTENT_PREFIX : DB_CACHE_SESSION_PREFIX;
  const cacheKey = `${cachePrefix}${url.pathname}${url.search}`;
  if (useCache) {
    const cached = readApiCache<T>(cacheStorage, cacheKey);
    if (cached !== undefined) {
      return cached;
    }
    const inflight = dbCacheInflight.get(cacheKey) as Promise<T> | undefined;
    if (inflight) {
      return inflight;
    }
  }

  const headers: Record<string, string> = { Accept: 'application/json' };
  const apiKey = getStoredApiKey();
  if (apiKey) {
    headers['X-API-Key'] = apiKey;
  }

  const controller = new AbortController();
  const timeoutMs = options.timeoutMs ?? 0;
  const timeoutID = timeoutMs > 0 ? window.setTimeout(() => controller.abort(), timeoutMs) : undefined;

  const request = fetch(url, { headers, signal: controller.signal })
    .then(async (response) => {
      if (!response.ok) {
        let message = `${response.status} ${response.statusText}`;
        try {
          const payload = await response.json();
          if (payload?.error) {
            message = payload.error;
          }
        } catch {
          // Keep the status text when the response is not JSON.
        }
        throw new Error(message);
      }
      return response.json() as Promise<T>;
    })
    .then((payload) => {
      if (useCache && ttlMs > 0) {
        writeApiCache(cacheStorage, cacheKey, payload, ttlMs);
      }
      return payload;
    })
    .catch((error: unknown) => {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw new Error('request timeout');
      }
      throw error;
    })
    .finally(() => {
      if (timeoutID !== undefined) {
        window.clearTimeout(timeoutID);
      }
      dbCacheInflight.delete(cacheKey);
    });

  if (useCache) {
    dbCacheInflight.set(cacheKey, request);
  }
  return request;
}

function clearStorageCache(storage: Storage, prefix: string) {
  for (let index = storage.length - 1; index >= 0; index -= 1) {
    const key = storage.key(index);
    if (key?.startsWith(prefix)) {
      storage.removeItem(key);
    }
  }
}

function readApiCache<T>(storage: Storage, key: string) {
  const raw = storage.getItem(key);
  if (!raw) return undefined;
  try {
    const entry = JSON.parse(raw) as DbCacheEntry<T>;
    if (entry.expiresAt <= Date.now()) {
      storage.removeItem(key);
      return undefined;
    }
    return entry.value;
  } catch {
    storage.removeItem(key);
    return undefined;
  }
}

function writeApiCache<T>(storage: Storage, key: string, value: T, ttlMs: number) {
  try {
    storage.setItem(key, JSON.stringify({ expiresAt: Date.now() + ttlMs, value } satisfies DbCacheEntry<T>));
  } catch {
    // Ignore quota errors; in-flight de-duplication still applies.
  }
}

export const api = {
  clearCache: clearApiCache,
  ready: () => apiGet<{ status: string }>('/ready', undefined, { cache: false }),
  datasets: () => apiGet<DatasetCatalogResponse>('/api/v1/infra/datasets', undefined, { ttlMs: 600_000, persistent: true, timeoutMs: 5_000 }),
  presets: () => apiGet<BrowserPresetResponse>('/api/v1/browser/presets', undefined, { ttlMs: 300_000, persistent: true }),
  schema: (dataset: string) => apiGet<BrowserSchemaResponse>(`/api/v1/browser/datasets/${dataset}/schema`, undefined, { ttlMs: 300_000, persistent: true }),
  preview: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserPreviewResponse>(`/api/v1/browser/datasets/${dataset}/preview`, params, { ttlMs: 30_000 }),
  coverage: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserCoverageResponse>(`/api/v1/browser/datasets/${dataset}/coverage`, params, { ttlMs: 120_000 }),
  profile: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserFieldProfileResponse>(`/api/v1/browser/datasets/${dataset}/field-profile`, params, { ttlMs: 120_000 }),
  validCount: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserValidCountResponse>(`/api/v1/browser/datasets/${dataset}/valid-count`, params, { ttlMs: 120_000 }),
  values: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserValueListResponse>(`/api/v1/browser/datasets/${dataset}/symbols`, params, { ttlMs: 120_000, persistent: true }),
  bars: (market: string, params: Record<string, string | number | undefined>) => apiGet<BarResponse>(`/api/v1/markets/${market}/bars`, params, { ttlMs: 30_000 }),
  marketSymbols: (market: string, params: Record<string, string | number | undefined>) => apiGet<MarketSymbolResponse>(`/api/v1/markets/${market}/symbols`, params, { ttlMs: 120_000, persistent: true }),
  fundamentalFactors: (market: string) => apiGet<FundamentalFactorCatalogResponse>('/api/v1/fundamentals/factors', { market }, { ttlMs: 600_000, persistent: true }),
  fundamentalSeries: (params: Record<string, string | number | undefined>) => apiGet<FundamentalSeriesResponse>('/api/v1/fundamentals/series', params, { ttlMs: 30_000, timeoutMs: 8_000 }),
  usOptionChain: (params: Record<string, string | number | undefined>) => apiGet<ChainResponse>('/api/v1/markets/us-options/chain', params),
  cryptoOptionChain: (params: Record<string, string | number | undefined>) => apiGet<ChainResponse>('/api/v1/markets/crypto-options/chain', params),
  cryptoOptionSymbols: (params: Record<string, string | number | undefined>) => apiGet<CryptoOptionSymbolResponse>('/api/v1/markets/crypto-options/symbols', params),
};
