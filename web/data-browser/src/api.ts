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
} from './types';

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? '';
const KEY_STORAGE = 'toktik.dataBrowser.apiKey';

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
}

export async function apiGet<T>(path: string, params?: Record<string, string | number | undefined>) {
  const url = new URL(`${API_BASE}${path}`, window.location.origin);
  for (const [key, value] of Object.entries(params ?? {})) {
    if (value !== undefined && `${value}`.trim() !== '') {
      url.searchParams.set(key, `${value}`);
    }
  }
  const headers: Record<string, string> = { Accept: 'application/json' };
  const apiKey = getStoredApiKey();
  if (apiKey) {
    headers['X-API-Key'] = apiKey;
  }
  const response = await fetch(url, { headers });
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
}

export const api = {
  ready: () => apiGet<{ status: string }>('/ready'),
  datasets: () => apiGet<DatasetCatalogResponse>('/api/v1/infra/datasets'),
  presets: () => apiGet<BrowserPresetResponse>('/api/v1/browser/presets'),
  schema: (dataset: string) => apiGet<BrowserSchemaResponse>(`/api/v1/browser/datasets/${dataset}/schema`),
  preview: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserPreviewResponse>(`/api/v1/browser/datasets/${dataset}/preview`, params),
  coverage: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserCoverageResponse>(`/api/v1/browser/datasets/${dataset}/coverage`, params),
  profile: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserFieldProfileResponse>(`/api/v1/browser/datasets/${dataset}/field-profile`, params),
  validCount: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserValidCountResponse>(`/api/v1/browser/datasets/${dataset}/valid-count`, params),
  values: (dataset: string, params: Record<string, string | number | undefined>) => apiGet<BrowserValueListResponse>(`/api/v1/browser/datasets/${dataset}/symbols`, params),
  bars: (market: string, params: Record<string, string | number | undefined>) => apiGet<BarResponse>(`/api/v1/markets/${market}/bars`, params),
  usOptionChain: (params: Record<string, string | number | undefined>) => apiGet<ChainResponse>('/api/v1/markets/us-options/chain', params),
  cryptoOptionChain: (params: Record<string, string | number | undefined>) => apiGet<ChainResponse>('/api/v1/markets/crypto-options/chain', params),
  cryptoOptionSymbols: (params: Record<string, string | number | undefined>) => apiGet<CryptoOptionSymbolResponse>('/api/v1/markets/crypto-options/symbols', params),
};
