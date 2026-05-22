import { useEffect, useMemo, useRef, useState } from 'react';
import type { Dispatch, ReactNode, SetStateAction } from 'react';
import { CandlestickSeries, ColorType, HistogramSeries, LineSeries, createChart } from 'lightweight-charts';
import type { CandlestickData, HistogramData, UTCTimestamp } from 'lightweight-charts';
import { Activity, Database, Play, RefreshCw, Save, Search } from 'lucide-react';
import { api, getStoredApiKey, setStoredApiKey } from './api';
import type {
  BarRow,
  BarResponse,
  BrowserColumn,
  BrowserCoverageResponse,
  BrowserDataset,
  BrowserFieldProfileResponse,
  BrowserPreviewResponse,
  BrowserSchemaResponse,
  BrowserValidCountResponse,
  BrowserValueListResponse,
  ChainResponse,
  DatasetCatalogResponse,
  FundamentalFactorCatalogEntry,
  FundamentalFactorCatalogResponse,
  FundamentalSeriesResponse,
  MarketSymbolResponse,
  MarketSymbolRow,
} from './types';

type Tab = 'schema' | 'symbols' | 'preview' | 'coverage' | 'profile' | 'validity' | 'market' | 'chain';

type QueryState<T> = {
  loading: boolean;
  error: string;
  data?: T;
};

type BrowserSelectionTarget = 'preview' | 'market' | 'chain';

type BrowserSelection = {
  id: number;
  target: BrowserSelectionTarget;
  field: string;
  value: string;
};

type SymbolSort = 'alpha' | 'rows' | 'recent';

type DateWindow = {
  from: string;
  to: string;
};

type ChartBar = {
  timestamp: string;
  time: UTCTimestamp;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  raw: BarRow;
};

type CatalogDataset = DatasetCatalogResponse['datasets'][number];

type BizMarketQueryRow = {
  biz_request_key: string;
  first_loaded_at: string;
  last_loaded_at: string;
  cache_status: string;
  hits: number;
  market: string;
  symbol: string;
  interval: string;
  from: string;
  to: string;
  limit: number;
  bars: number;
  last_price: string;
};

type BizMarketGapRow = {
  biz_gap: string;
  from: string;
  to: string;
  missing_span: string;
};

type BizBootstrapCache = {
  cachedAt: number;
  datasets: BrowserDataset[];
  catalog?: DatasetCatalogResponse;
};

type MarketSeriesResult = {
  market: string;
  response: BarResponse;
  fundamentals: FundamentalSeriesResponse | undefined;
};

type FundamentalChartPoint = {
  eventTS: string;
  knownAt: string;
  time: UTCTimestamp;
  value: number;
  filled: boolean;
  source?: string;
  revision?: number;
};

const defaultIntervals = ['1m', '5m', '15m', '30m', '1h', '2h', '3h', '4h', '6h', '8h', '12h', '1d'];
const forexIntervals = ['1m', '5m', '15m', '30m', '1h', '2h', '4h', '1d'];
const bizBootstrapCacheKey = 'toktik.biz_data_browser_bootstrap.v2';
const bizBootstrapCacheMaxAgeMs = 1000 * 60 * 60 * 6;

const tabs: Array<{ id: Tab; label: string }> = [
  { id: 'schema', label: 'Schema' },
  { id: 'symbols', label: 'Symbols' },
  { id: 'preview', label: 'Preview' },
  { id: 'coverage', label: 'Coverage' },
  { id: 'profile', label: 'Field Profile' },
  { id: 'validity', label: 'Valid Count' },
  { id: 'market', label: 'Market Series' },
  { id: 'chain', label: 'Option Chain' },
];

const fallbackDateWindow = (() => {
  const to = new Date();
  const from = new Date(to.getTime() - 1000 * 60 * 60 * 24 * 30);
  return { from: toDateInput(from), to: toDateInput(to) };
})();

export default function App() {
  const cachedBootstrap = readBizBootstrapCache();
  const [apiKey, setApiKey] = useState(getStoredApiKey());
  const [status, setStatus] = useState<'checking' | 'ready' | 'error'>(cachedBootstrap ? 'ready' : 'checking');
  const [statusError, setStatusError] = useState(cachedBootstrap?.catalog ? '' : '');
  const [datasets, setDatasets] = useState<BrowserDataset[]>(cachedBootstrap?.datasets ?? []);
  const [catalog, setCatalog] = useState<DatasetCatalogResponse | undefined>(cachedBootstrap?.catalog);
  const [activeName, setActiveName] = useState('');
  const [filter, setFilter] = useState('');
  const [tab, setTab] = useState<Tab>('schema');
  const [refreshToken, setRefreshToken] = useState(0);
  const [browserSelection, setBrowserSelection] = useState<BrowserSelection>();

  useEffect(() => {
    refreshBootstrap();
  }, [refreshToken]);

  const activeDataset = useMemo(
    () => datasets.find((dataset) => dataset.name === activeName) ?? datasets[0],
    [activeName, datasets],
  );

  const activeCatalogDataset = useMemo(
    () => catalog?.datasets.find((dataset) => dataset.name === activeDataset?.name),
    [activeDataset?.name, catalog],
  );

  useEffect(() => {
    if (!activeName && datasets.length > 0) {
      setActiveName(datasets[0].name);
    }
  }, [activeName, datasets]);

  const filteredDatasets = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) {
      return datasets;
    }
    return datasets.filter((dataset) => `${dataset.name} ${dataset.market} ${dataset.relation}`.toLowerCase().includes(needle));
  }, [datasets, filter]);

  async function refreshBootstrap() {
    if (!cachedBootstrap) {
      setStatus('checking');
    }
    setStatusError('');
    try {
      const [ready, presets] = await Promise.all([api.ready(), api.presets()]);
      setStatus(ready.status === 'ready' ? 'ready' : 'error');
      setDatasets(presets.datasets);
      writeBizBootstrapCache({ cachedAt: Date.now(), datasets: presets.datasets, catalog: cachedBootstrap?.catalog });

      void api.datasets()
        .then((catalogResp) => {
          setCatalog(catalogResp);
          setStatusError('');
          writeBizBootstrapCache({ cachedAt: Date.now(), datasets: presets.datasets, catalog: catalogResp });
        })
        .catch((error) => {
          if (!cachedBootstrap?.catalog) {
            setStatusError(`Dataset catalog unavailable: ${errorMessage(error)}. Showing preset-only view.`);
          }
        });
    } catch (error) {
      setStatus('error');
      setStatusError(errorMessage(error));
    }
  }

  function saveApiKey() {
    setStoredApiKey(apiKey);
    clearBizBootstrapCache();
    setRefreshToken((value) => value + 1);
  }

  function refreshAll() {
    api.clearCache();
    clearBizBootstrapCache();
    setRefreshToken((value) => value + 1);
  }

  function handleValueSelection(target: BrowserSelectionTarget, field: string, value: string) {
    setBrowserSelection({ id: Date.now(), target, field, value });
    setTab(target);
  }

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="sidebar-header">
          <div className="brand-row">
            <h1>Toktik Data Browser</h1>
            <span className={`status-pill ${status === 'error' ? 'error' : ''}`}>{status === 'checking' ? 'checking' : status}</span>
          </div>
          <div className="api-key-row">
            <input
              aria-label="API key"
              placeholder="X-API-Key"
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
              type="password"
            />
            <button className="icon-button" onClick={saveApiKey} title="Save API key" aria-label="Save API key">
              <Save size={16} />
            </button>
          </div>
          {statusError && <div className="error-state">{statusError}</div>}
        </div>
        <div className="dataset-filter">
          <input
            className="filter-input"
            placeholder="Filter datasets"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </div>
        <div className="dataset-list">
          {filteredDatasets.map((dataset) => (
            <button
              key={dataset.name}
              className={`dataset-button ${activeDataset?.name === dataset.name ? 'active' : ''}`}
              onClick={() => setActiveName(dataset.name)}
            >
              <span className="dataset-name">{dataset.name}</span>
              <span className="dataset-meta">{dataset.market} / {dataset.relation}</span>
            </button>
          ))}
        </div>
      </aside>

      <main className="main">
        <header className="topbar">
          <div className="title-stack">
            <h2>{activeDataset?.name ?? 'No dataset selected'}</h2>
            <p>{activeDataset ? `${activeDataset.market} · ${activeDataset.relation}` : 'Start the API server to load browser presets.'}</p>
          </div>
          <div className="summary-strip">
            <Metric label="ready" value={catalog?.summary.ready ?? 0} />
            <Metric label="stale" value={catalog?.summary.stale ?? 0} />
            <Metric label="missing" value={catalog?.summary.missing ?? 0} />
            <button className="icon-button" onClick={refreshAll} title="Refresh" aria-label="Refresh">
              <RefreshCw size={16} />
            </button>
          </div>
        </header>

        <nav className="tabs">
          {tabs.map((item) => (
            <button key={item.id} className={`tab ${tab === item.id ? 'active' : ''}`} onClick={() => setTab(item.id)}>
              {item.label}
            </button>
          ))}
        </nav>

        <section className="workspace">
          {!activeDataset ? (
            <div className="panel"><div className="empty-state">No browser presets loaded.</div></div>
          ) : (
            <TabPanel dataset={activeDataset} catalogDataset={activeCatalogDataset} tab={tab} selection={browserSelection} onSelectValue={handleValueSelection} />
          )}
        </section>
      </main>
    </div>
  );
}

function TabPanel({ dataset, catalogDataset, tab, selection, onSelectValue }: { dataset: BrowserDataset; catalogDataset?: CatalogDataset; tab: Tab; selection?: BrowserSelection; onSelectValue: (target: BrowserSelectionTarget, field: string, value: string) => void }) {
  const panelKey = `${tab}:${dataset.name}`;
  const dateWindow = defaultDateWindow(catalogDataset?.last_timestamp);

  if (tab === 'schema') return <SchemaPanel key={panelKey} dataset={dataset} />;
  if (tab === 'symbols') return <SymbolsPanel key={panelKey} dataset={dataset} onSelectValue={onSelectValue} />;
  if (tab === 'preview') return <PreviewPanel key={panelKey} dataset={dataset} dateWindow={dateWindow} selection={selection} />;
  if (tab === 'coverage') return <CoveragePanel key={panelKey} dataset={dataset} dateWindow={dateWindow} />;
  if (tab === 'profile') return <ProfilePanel key={panelKey} dataset={dataset} dateWindow={dateWindow} />;
  if (tab === 'validity') return <ValidityPanel key={panelKey} dataset={dataset} dateWindow={dateWindow} />;
  if (tab === 'market') return <MarketPanel key={panelKey} dataset={dataset} catalogDataset={catalogDataset} dateWindow={dateWindow} selection={selection} />;
  return <ChainPanel key={panelKey} dataset={dataset} dateWindow={dateWindow} selection={selection} />;
}

function SchemaPanel({ dataset }: { dataset: BrowserDataset }) {
  const [state, setState] = useQueryState<BrowserSchemaResponse>();

  useEffect(() => {
    runQuery(setState, () => api.schema(dataset.name));
  }, [dataset.name, setState]);

  return (
    <Panel title="Dataset Schema" icon={<Database size={16} />} loading={state.loading} error={state.error}>
      {state.data && <ColumnTable columns={state.data.columns} />}
    </Panel>
  );
}

function SymbolsPanel({ dataset, onSelectValue }: { dataset: BrowserDataset; onSelectValue: (target: BrowserSelectionTarget, field: string, value: string) => void }) {
  const [search, setSearch] = useState('');
  const [limit, setLimit] = useState(500);
  const [sortBy, setSortBy] = useState<SymbolSort>('alpha');
  const [state, setState] = useQueryState<BrowserValueListResponse>();

  useEffect(() => {
    runQuery(setState, () => api.values(dataset.name, { search, limit }));
  }, [dataset.name, search, limit, setState]);

  return (
    <Panel title="Symbols And Underlyings" icon={<Database size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, () => api.values(dataset.name, { search, limit }))} />}>
      <QueryControls>
        <Control label="Search"><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="AAPL, SPY, BTC" /></Control>
        <Control label="Limit"><input value={limit} onChange={(event) => setLimit(Number(event.target.value))} type="number" min={1} max={5000} /></Control>
        <Control label="Sort">
          <select value={sortBy} onChange={(event) => setSortBy(event.target.value as SymbolSort)}>
            <option value="alpha">Alphabetical</option>
            <option value="rows">Row Count</option>
            <option value="recent">Latest Timestamp</option>
          </select>
        </Control>
      </QueryControls>
      {state.data && <ValueListView dataset={dataset} data={state.data} sortBy={sortBy} onSelectValue={onSelectValue} />}
    </Panel>
  );
}

function PreviewPanel({ dataset, dateWindow, selection }: { dataset: BrowserDataset; dateWindow: DateWindow; selection?: BrowserSelection }) {
  const [symbol, setSymbol] = useState('');
  const [underlying, setUnderlying] = useState('');
  const [from, setFrom] = useState(dateWindow.from);
  const [to, setTo] = useState(dateWindow.to);
  const [columns, setColumns] = useState(dataset.fields?.join(',') ?? '');
  const [limit, setLimit] = useState(100);
  const [state, setState] = useQueryState<BrowserPreviewResponse>();

  useEffect(() => {
    setColumns(dataset.fields?.join(',') ?? '');
  }, [dataset.name, dataset.fields]);

  useEffect(() => {
    if (!selection || selection.target !== 'preview' || !selection.value.trim()) {
      return;
    }
    applySelectionToFilters(dataset, selection, setSymbol, setUnderlying);
  }, [dataset, selection]);

  return (
    <Panel title="Row Preview" icon={<Search size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, () => api.preview(dataset.name, { symbol, underlying, from, to, columns, limit }))} />}>
      <QueryControls>
        <Control label="Symbol"><input value={symbol} onChange={(event) => setSymbol(event.target.value)} placeholder="AAPL or contract" /></Control>
        <Control label="Underlying"><input value={underlying} onChange={(event) => setUnderlying(event.target.value)} placeholder="SPY, BTC" /></Control>
        <Control label="From"><input value={from} onChange={(event) => setFrom(event.target.value)} type="date" /></Control>
        <Control label="To"><input value={to} onChange={(event) => setTo(event.target.value)} type="date" /></Control>
        <Control label="Limit"><input value={limit} onChange={(event) => setLimit(Number(event.target.value))} type="number" min={1} max={1000} /></Control>
        <Control label="Columns"><input value={columns} onChange={(event) => setColumns(event.target.value)} /></Control>
      </QueryControls>
      {state.data && <DataTable rows={state.data.data} columns={state.data.columns} />}
    </Panel>
  );
}

function CoveragePanel({ dataset, dateWindow }: { dataset: BrowserDataset; dateWindow: DateWindow }) {
  const [symbol, setSymbol] = useState('');
  const [underlying, setUnderlying] = useState('');
  const [from, setFrom] = useState(dateWindow.from);
  const [to, setTo] = useState(dateWindow.to);
  const [state, setState] = useQueryState<BrowserCoverageResponse>();

  return (
    <Panel title="Time Coverage" icon={<Activity size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, () => api.coverage(dataset.name, { symbol, underlying, from, to }))} />}>
      <QueryControls>
        <Control label="Symbol"><input value={symbol} onChange={(event) => setSymbol(event.target.value)} /></Control>
        <Control label="Underlying"><input value={underlying} onChange={(event) => setUnderlying(event.target.value)} /></Control>
        <Control label="From"><input value={from} onChange={(event) => setFrom(event.target.value)} type="date" /></Control>
        <Control label="To"><input value={to} onChange={(event) => setTo(event.target.value)} type="date" /></Control>
      </QueryControls>
      {state.data && <CoverageView data={state.data} />}
    </Panel>
  );
}

function ProfilePanel({ dataset, dateWindow }: { dataset: BrowserDataset; dateWindow: DateWindow }) {
  const [field, setField] = useState(dataset.fields?.[0] ?? '');
  const [from, setFrom] = useState(dateWindow.from);
  const [to, setTo] = useState(dateWindow.to);
  const [state, setState] = useQueryState<BrowserFieldProfileResponse>();

  useEffect(() => {
    setField(dataset.fields?.[0] ?? '');
  }, [dataset.name, dataset.fields]);

  return (
    <Panel title="Field Profile" icon={<Search size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, () => api.profile(dataset.name, { field, from, to }))} />}>
      <QueryControls>
        <Control label="Field">
          <select value={field} onChange={(event) => setField(event.target.value)}>
            {(dataset.fields ?? []).map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
        </Control>
        <Control label="From"><input value={from} onChange={(event) => setFrom(event.target.value)} type="date" /></Control>
        <Control label="To"><input value={to} onChange={(event) => setTo(event.target.value)} type="date" /></Control>
      </QueryControls>
      {state.data && <ProfileView data={state.data} />}
    </Panel>
  );
}

function ValidityPanel({ dataset, dateWindow }: { dataset: BrowserDataset; dateWindow: DateWindow }) {
  const [check, setCheck] = useState(dataset.checks?.[0] ?? 'default');
  const [from, setFrom] = useState(dateWindow.from);
  const [to, setTo] = useState(dateWindow.to);
  const [state, setState] = useQueryState<BrowserValidCountResponse>();

  useEffect(() => {
    setCheck(dataset.checks?.[0] ?? 'default');
  }, [dataset.name, dataset.checks]);

  return (
    <Panel title="Valid Row Count" icon={<Activity size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, () => api.validCount(dataset.name, { check, from, to }))} />}>
      <QueryControls>
        <Control label="Check">
          <select value={check} onChange={(event) => setCheck(event.target.value)}>
            {(dataset.checks ?? ['default']).map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
        </Control>
        <Control label="From"><input value={from} onChange={(event) => setFrom(event.target.value)} type="date" /></Control>
        <Control label="To"><input value={to} onChange={(event) => setTo(event.target.value)} type="date" /></Control>
      </QueryControls>
      {state.data && <ValidityView data={state.data} />}
    </Panel>
  );
}

function MarketPanel({ dataset, catalogDataset, dateWindow, selection }: { dataset: BrowserDataset; catalogDataset?: CatalogDataset; dateWindow: DateWindow; selection?: BrowserSelection }) {
  const market = browserDatasetToMarket(dataset);
  const supportsFundamentals = market === 'us-stocks';
  const [symbol, setSymbol] = useState(defaultMarketSymbol(dataset));
  const [symbolSearch, setSymbolSearch] = useState(defaultMarketSymbol(dataset));
  const [interval, setInterval] = useState('1h');
  const [from, setFrom] = useState(dateWindow.from);
  const [to, setTo] = useState(dateWindow.to);
  const [limit, setLimit] = useState(1000);
  const [factor, setFactor] = useState('none');
  const [state, setState] = useQueryState<MarketSeriesResult>();
  const [symbolState, setSymbolState] = useQueryState<MarketSymbolResponse>();
  const [factorState, setFactorState] = useQueryState<FundamentalFactorCatalogResponse>();
  const [bizQueryRows, setBizQueryRows] = useState<BizMarketQueryRow[]>([]);
  const symbolRequestIDRef = useRef(0);
  const intervals = marketIntervals(market);
  const symbolListID = `market-symbols-${market}`;
  const bizDatasetRows = useMemo(() => [toBizDatasetRow(dataset, catalogDataset, intervals)], [dataset, catalogDataset, intervals]);
  const factorOptions = useMemo(
    () => (factorState.data?.data ?? []).filter((entry) => entry.active && entry.market === 'us-stocks'),
    [factorState.data],
  );
  const selectedFactor = useMemo(
    () => factorOptions.find((entry) => entry.factor_code === factor),
    [factor, factorOptions],
  );

  useEffect(() => {
    const fallback = defaultMarketSymbol(dataset);
    setSymbol(fallback);
    setSymbolSearch(fallback);
  }, [dataset.name]);

  useEffect(() => {
    if (!intervals.includes(interval)) {
      setInterval(defaultMarketInterval(market));
    }
  }, [interval, intervals, market]);

  useEffect(() => {
    if (!supportsFundamentals) {
      setFactor('none');
      return;
    }
    runQuery(setFactorState, () => api.fundamentalFactors('us-stocks'));
  }, [supportsFundamentals, setFactorState]);

  useEffect(() => {
    if (!supportsFundamentals) {
      return;
    }
    if (factorOptions.length === 0) {
      setFactor('none');
      return;
    }
    if (factor !== 'none' && factorOptions.some((entry) => entry.factor_code === factor)) {
      return;
    }
    setFactor(factorOptions.find((entry) => entry.factor_code === 'pe')?.factor_code ?? factorOptions[0].factor_code);
  }, [factor, factorOptions, supportsFundamentals]);

  useEffect(() => {
    const handle = setTimeout(() => {
      const requestID = symbolRequestIDRef.current + 1;
      symbolRequestIDRef.current = requestID;
      setSymbolState((current) => ({ ...current, loading: true, error: '' }));
      api.marketSymbols(market, { search: symbolSearch, limit: 25 })
        .then((data) => {
          if (symbolRequestIDRef.current !== requestID) {
            return;
          }
          setSymbolState({ loading: false, error: '', data });
        })
        .catch((error) => {
          if (symbolRequestIDRef.current !== requestID) {
            return;
          }
          setSymbolState((current) => ({ ...current, loading: false, error: errorMessage(error) }));
        });
    }, 250);
    return () => clearTimeout(handle);
  }, [market, symbolSearch, setSymbolState]);

  useEffect(() => {
    let cancelled = false;
    resolveDefaultMarketSymbol(dataset, dateWindow)
      .then((nextSymbol) => {
        if (!cancelled && nextSymbol) {
          setSymbol(nextSymbol);
          setSymbolSearch(nextSymbol);
        }
      })
      .catch(() => {
        // Keep the fallback symbol when lookup fails.
      });
    return () => {
      cancelled = true;
    };
  }, [dataset, dateWindow]);

  useEffect(() => {
    if (!selection || selection.target !== 'market' || !selection.value.trim()) {
      return;
    }
    setSymbol(selection.value);
    setSymbolSearch(selection.value);
  }, [selection]);

  async function runMarketBars() {
    const response = await api.bars(market, { symbol, interval, from, to, limit });
    const fundamentals = supportsFundamentals && factor !== 'none'
      ? await api.fundamentalSeries({ market: 'us-stocks', symbol, factor, from, to, mode: 'filled' })
      : undefined;
    const result = { market, response, fundamentals };
    const bars = toChartBars(result.response.data);
    const requestKey = marketQueryKey({ market, symbol, interval, from, to, limit });
    setBizQueryRows((current) => upsertBizQueryRows(current, {
        biz_request_key: requestKey,
        first_loaded_at: new Date().toISOString(),
        last_loaded_at: new Date().toISOString(),
        cache_status: current.some((row) => row.biz_request_key === requestKey) ? 'hit' : 'miss',
        hits: 1,
        market,
        symbol,
        interval,
        from,
        to,
        limit,
        bars: bars.length,
        last_price: bars.length > 0 ? formatPrice(bars[bars.length - 1].close) : 'n/a',
      }).slice(0, 8));
    return result;
  }

  return (
    <Panel title="Market Series" icon={<Activity size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, runMarketBars)} />}>
      <QueryControls className="market-controls">
        <Control label="Market"><input value={market} readOnly /></Control>
        <Control label="Symbol">
          <input
            list={symbolListID}
            value={symbol}
            onChange={(event) => {
              setSymbol(event.target.value);
              setSymbolSearch(event.target.value);
            }}
            placeholder="AAPL, BTC, EURUSD"
          />
          <datalist id={symbolListID}>
            {(symbolState.data?.data ?? []).map((row, index) => {
              const value = marketSymbolValue(row);
              return value ? <option key={`${value}:${index}`} value={value}>{marketSymbolLabel(row)}</option> : null;
            })}
          </datalist>
        </Control>
        <Control label="Interval">
          <select value={interval} onChange={(event) => setInterval(event.target.value)}>
            {intervals.map((value) => <option key={value} value={value}>{value}</option>)}
          </select>
        </Control>
        <Control label="From"><input value={from} onChange={(event) => setFrom(event.target.value)} type="date" /></Control>
        <Control label="To"><input value={to} onChange={(event) => setTo(event.target.value)} type="date" /></Control>
        <Control label="Limit"><input value={limit} onChange={(event) => setLimit(Number(event.target.value))} type="number" min={50} max={10000} step={50} /></Control>
        {supportsFundamentals && (
          <Control label="Fundamental">
            <select value={factor} onChange={(event) => setFactor(event.target.value)}>
              <option value="none">none</option>
              {factorOptions.map((entry) => <option key={entry.factor_code} value={entry.factor_code}>{fundamentalFactorLabel(entry)}</option>)}
            </select>
          </Control>
        )}
      </QueryControls>
      {supportsFundamentals && factorState.error && <div className="error-state inline-error">Fundamentals catalog unavailable: {factorState.error}</div>}
      <div className="biz-table-grid">
        <BizTable title="biz_market_dataset" rows={bizDatasetRows} columns={['biz_table', 'market', 'relation', 'status', 'freshness', 'row_count', 'last_timestamp', 'intervals']} />
        <BizTable title="biz_market_query_cache" rows={bizQueryRows} columns={['biz_request_key', 'cache_status', 'hits', 'market', 'symbol', 'interval', 'bars', 'last_price', 'last_loaded_at']} emptyText="Run a market query to populate the prefixed business cache table." />
      </div>
      {state.data && <SeriesView rows={state.data.response.data} interval={interval} fundamentals={state.data.fundamentals} factorEntry={selectedFactor} />}
    </Panel>
  );
}

function ChainPanel({ dataset, dateWindow, selection }: { dataset: BrowserDataset; dateWindow: DateWindow; selection?: BrowserSelection }) {
  const isCrypto = dataset.market === 'crypto-options';
  const isUS = dataset.market === 'us-options';
  const [underlying, setUnderlying] = useState(isCrypto ? 'BTC' : 'SPY');
  const [from, setFrom] = useState(dateWindow.from);
  const [to, setTo] = useState(dateWindow.to);
  const [expiration, setExpiration] = useState('');
  const [state, setState] = useQueryState<ChainResponse>();

  useEffect(() => {
    if (!selection || selection.target !== 'chain' || !selection.value.trim()) {
      return;
    }
    setUnderlying(selection.value);
  }, [selection]);

  if (!isCrypto && !isUS) {
    return <Panel title="Option Chain" icon={<Database size={16} />}><div className="empty-state">Select an options dataset to query chain snapshots.</div></Panel>;
  }

  return (
    <Panel title="Option Chain" icon={<Database size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, () => isCrypto ? api.cryptoOptionChain({ base_asset: underlying, from, to, interval: '1d', limit: 3 }) : api.usOptionChain({ underlying, expiration, from, to, interval: '1d', limit: 3 }))} />}>
      <QueryControls>
        <Control label={isCrypto ? 'Base asset' : 'Underlying'}><input value={underlying} onChange={(event) => setUnderlying(event.target.value)} /></Control>
        {!isCrypto && <Control label="Expiration"><input value={expiration} onChange={(event) => setExpiration(event.target.value)} placeholder="YYYY-MM-DD" /></Control>}
        <Control label="From"><input value={from} onChange={(event) => setFrom(event.target.value)} type="date" /></Control>
        <Control label="To"><input value={to} onChange={(event) => setTo(event.target.value)} type="date" /></Control>
      </QueryControls>
      {state.data && <ChainView data={state.data} />}
    </Panel>
  );
}

function Panel({ title, icon, action, loading, error, children }: { title: string; icon?: ReactNode; action?: ReactNode; loading?: boolean; error?: string; children: ReactNode }) {
  return (
    <div className="panel">
      <div className="panel-header">
        <h3>{icon} {title}</h3>
        {action}
      </div>
      <div className="panel-body">
        {loading && <div className="loading-state">Loading...</div>}
        {error && !loading && <div className="error-state">{error}</div>}
        {(!error || loading) && children}
      </div>
    </div>
  );
}

function RunButton({ onClick }: { onClick: () => void }) {
  return <button className="primary-button" onClick={onClick}><Play size={15} />Run</button>;
}

function QueryControls({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`controls ${className}`.trim()}>{children}</div>;
}

function Control({ label, children }: { label: string; children: ReactNode }) {
  return <div className="control"><label>{label}</label>{children}</div>;
}

function Metric({ label, value }: { label: string; value: number }) {
  return <div className="metric-chip"><strong>{value}</strong><span>{label}</span></div>;
}

function ColumnTable({ columns }: { columns: BrowserColumn[] }) {
  return <DataTable rows={columns as unknown as Array<Record<string, unknown>>} columns={['position', 'name', 'type', 'is_nullable', 'default_kind', 'comment', 'codec_expression']} />;
}

function CoverageView({ data }: { data: BrowserCoverageResponse }) {
  return (
    <>
      <div className="coverage-grid">
        <Stat label="Rows" value={formatNumber(data.row_count)} />
        <Stat label="First timestamp" value={data.first_timestamp ?? 'n/a'} />
        <Stat label="Last timestamp" value={data.last_timestamp ?? 'n/a'} />
        <Stat label="Days" value={formatNumber(data.daily?.length ?? 0)} />
      </div>
      <DataTable rows={data.daily ?? []} columns={['date', 'row_count']} />
    </>
  );
}

function ProfileView({ data }: { data: BrowserFieldProfileResponse }) {
  return (
    <div className="profile-grid">
      <Stat label="Rows" value={formatNumber(data.row_count)} />
      <Stat label="Nulls" value={formatNumber(data.null_count)} />
      <Stat label="Distinct" value={formatNumber(data.distinct_count)} />
      <Stat label="Type" value={data.type} />
      {data.zero_count !== undefined && <Stat label="Zero" value={formatNumber(data.zero_count)} />}
      {data.empty_count !== undefined && <Stat label="Empty" value={formatNumber(data.empty_count)} />}
      <Stat label="Min" value={valueText(data.min)} />
      <Stat label="Max" value={valueText(data.max)} />
    </div>
  );
}

function ValidityView({ data }: { data: BrowserValidCountResponse }) {
  const validPct = data.row_count > 0 ? Math.round((data.valid_count / data.row_count) * 10000) / 100 : 0;
  return (
    <div className="profile-grid">
      <Stat label="Rows" value={formatNumber(data.row_count)} />
      <Stat label="Valid" value={formatNumber(data.valid_count)} />
      <Stat label="Invalid" value={formatNumber(data.invalid_count)} />
      <Stat label="Valid %" value={`${validPct}%`} />
    </div>
  );
}

function SeriesView({ rows, interval, fundamentals, factorEntry }: { rows: BarRow[]; interval: string; fundamentals?: FundamentalSeriesResponse; factorEntry?: FundamentalFactorCatalogEntry }) {
  const bars = toChartBars(rows);
  const gaps = toGapRows(bars, interval);
  const latest = bars.length > 0 ? bars[bars.length - 1] : undefined;
  const first = bars[0];
  const volume = bars.reduce((total, bar) => total + bar.volume, 0);
  return (
    <>
      <div className="market-summary">
        <Stat label="Last" value={latest ? formatPrice(latest.close) : 'n/a'} />
        <Stat label="Bars" value={formatNumber(bars.length)} />
        <Stat label="Volume" value={formatCompactNumber(volume)} />
        <Stat label="Window" value={first && latest ? `${compactDate(first.timestamp)} - ${compactDate(latest.timestamp)}` : 'n/a'} />
      </div>
      {gaps.length > 0 && (
        <BizTable title="biz_market_gap_diagnostics" rows={gaps.slice(0, 12)} columns={['biz_gap', 'from', 'to', 'missing_span']} />
      )}
      <div className="chart market-chart"><MarketCandlestickChart bars={bars} /></div>
      {fundamentals && <FundamentalSeriesSection response={fundamentals} factorEntry={factorEntry} />}
      <DataTable rows={rows.slice(0, 200) as Array<Record<string, unknown>>} columns={inferColumns(rows)} />
    </>
  );
}

function FundamentalSeriesSection({ response, factorEntry }: { response: FundamentalSeriesResponse; factorEntry?: FundamentalFactorCatalogEntry }) {
  const points = toFundamentalChartPoints(response.data);
  const latest = points.length > 0 ? points[points.length - 1] : undefined;
  const filledCount = points.filter((point) => point.filled).length;
  const tableRows = response.data.slice(-200).reverse().map((point) => ({
    event_ts: point.event_ts,
    known_at: point.known_at,
    value: formatFundamentalValue(point.value, factorEntry),
    filled: point.filled ? 'yes' : 'no',
    source: point.source ?? '',
    revision: point.revision ?? '',
  }));

  return (
    <section className="fundamentals-section">
      <div className="fundamentals-header">
        <h4>{factorEntry?.display_name ?? response.factor} fundamentals</h4>
        <div className="fundamental-chip-row">
          <span className="subtle-chip">symbol {response.symbol}</span>
          <span className="subtle-chip">mode {response.mode}</span>
          {response.fill_policy && <span className="subtle-chip">fill {response.fill_policy}</span>}
        </div>
      </div>
      <div className="coverage-grid fundamentals-summary">
        <Stat label="Latest" value={latest ? formatFundamentalValue(latest.value, factorEntry) : 'n/a'} />
        <Stat label="Points" value={formatNumber(points.length)} />
        <Stat label="Filled" value={formatNumber(filledCount)} />
        <Stat label="Latest event" value={latest ? compactDate(latest.eventTS) : 'n/a'} />
      </div>
      <div className="chart fundamentals-chart"><FundamentalLineChart points={points} label={factorEntry?.display_name ?? response.factor} /></div>
      <BizTable title="biz_fundamental_series" rows={tableRows} columns={['event_ts', 'known_at', 'value', 'filled', 'source', 'revision']} emptyText="No fundamentals data returned for the selected symbol and date window." />
    </section>
  );
}

function ChainView({ data }: { data: ChainResponse }) {
  const snapshot = data.data[0];
  if (!snapshot) {
    return <div className="empty-state">No chain snapshots returned.</div>;
  }
  return (
    <>
      <div className="coverage-grid">
        <Stat label="Timestamp" value={snapshot.timestamp} />
        <Stat label="Contracts" value={formatNumber(snapshot.contracts.length)} />
      </div>
      <DataTable rows={snapshot.contracts.slice(0, 500)} columns={inferColumns(snapshot.contracts)} />
    </>
  );
}

function BizTable({ title, rows, columns, emptyText = 'No business rows yet.' }: { title: string; rows: Array<Record<string, unknown>>; columns: string[]; emptyText?: string }) {
  return (
    <section className="biz-table-card" aria-label={title}>
      <div className="biz-table-title">{title}</div>
      {rows.length ? <DataTable rows={rows} columns={columns} /> : <div className="empty-state compact">{emptyText}</div>}
    </section>
  );
}

function ValueListView({ dataset, data, sortBy, onSelectValue }: { dataset: BrowserDataset; data: BrowserValueListResponse; sortBy: SymbolSort; onSelectValue: (target: BrowserSelectionTarget, field: string, value: string) => void }) {
  if (!data.fields.length) {
    return <div className="empty-state">This dataset does not expose a symbol or underlying field.</div>;
  }
  return (
    <>
      <div className="coverage-grid">
        <Stat label="Field groups" value={formatNumber(data.fields.length)} />
        <Stat label="Cached" value={data.cached ? 'yes' : 'no'} />
      </div>
      {data.fields.map((fieldGroup) => (
        <div key={fieldGroup.field}>
          <h4>{fieldGroup.field}</h4>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>value</th>
                  <th>row_count</th>
                  <th>last_timestamp</th>
                  <th>actions</th>
                </tr>
              </thead>
              <tbody>
                {sortBrowserValues(fieldGroup.values, sortBy).map((item) => (
                  <tr key={`${fieldGroup.field}:${item.value}`}>
                    <td>
                      <button className="link-button" onClick={() => onSelectValue('preview', fieldGroup.field, item.value)}>{item.value}</button>
                    </td>
                    <td>{formatNumber(item.row_count)}</td>
                    <td>{item.last_timestamp ?? ''}</td>
                    <td>
                      <div className="inline-actions">
                        <button className="mini-button" onClick={() => onSelectValue('preview', fieldGroup.field, item.value)}>Preview</button>
                        {canOpenMarket(dataset, fieldGroup.field) && <button className="mini-button" onClick={() => onSelectValue('market', fieldGroup.field, item.value)}>Market</button>}
                        {canOpenChain(dataset, fieldGroup.field) && <button className="mini-button" onClick={() => onSelectValue('chain', fieldGroup.field, item.value)}>Chain</button>}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ))}
    </>
  );
}

function MarketCandlestickChart({ bars }: { bars: ChartBar[] }) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || bars.length < 2) {
      return;
    }

    const chart = createChart(container, {
      width: container.clientWidth,
      height: 420,
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: '#fbfcfd' },
        textColor: '#415066',
      },
      grid: {
        vertLines: { color: '#edf2f6' },
        horzLines: { color: '#edf2f6' },
      },
      rightPriceScale: {
        borderColor: '#d8e0e7',
        scaleMargins: { top: 0.08, bottom: 0.26 },
      },
      timeScale: {
        borderColor: '#d8e0e7',
        timeVisible: true,
        secondsVisible: false,
      },
      crosshair: {
        vertLine: { color: '#9aa9b8', labelBackgroundColor: '#155d8b' },
        horzLine: { color: '#9aa9b8', labelBackgroundColor: '#155d8b' },
      },
    });

    const candleSeries = chart.addSeries(CandlestickSeries, {
      upColor: '#1f8a5b',
      downColor: '#c44536',
      borderUpColor: '#1f8a5b',
      borderDownColor: '#c44536',
      wickUpColor: '#1f8a5b',
      wickDownColor: '#c44536',
    });
    candleSeries.setData(bars.map(toCandlestickData));

    const volumeSeries = chart.addSeries(HistogramSeries, {
      priceFormat: { type: 'volume' },
      priceScaleId: '',
    });
    volumeSeries.setData(bars.map(toVolumeData));
    volumeSeries.priceScale().applyOptions({ scaleMargins: { top: 0.82, bottom: 0 } });

    chart.timeScale().fitContent();

    return () => {
      chart.remove();
    };
  }, [bars]);

  if (bars.length < 2) {
    return <div className="empty-state">Run a query with at least two valid OHLC bars.</div>;
  }
  return <div className="market-chart-canvas" ref={containerRef} aria-label="Candlestick price chart" />;
}

function FundamentalLineChart({ points, label }: { points: FundamentalChartPoint[]; label: string }) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || points.length === 0) {
      return;
    }

    const chart = createChart(container, {
      width: container.clientWidth,
      height: 220,
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: '#fbfcfd' },
        textColor: '#415066',
      },
      grid: {
        vertLines: { color: '#edf2f6' },
        horzLines: { color: '#edf2f6' },
      },
      rightPriceScale: {
        borderColor: '#d8e0e7',
        scaleMargins: { top: 0.12, bottom: 0.12 },
      },
      timeScale: {
        borderColor: '#d8e0e7',
        timeVisible: true,
        secondsVisible: false,
      },
      crosshair: {
        vertLine: { color: '#9aa9b8', labelBackgroundColor: '#155d8b' },
        horzLine: { color: '#9aa9b8', labelBackgroundColor: '#155d8b' },
      },
    });

    const lineSeries = chart.addSeries(LineSeries, {
      color: '#7f3c8d',
      lineWidth: 2,
      crosshairMarkerRadius: 4,
      lastValueVisible: true,
      priceLineVisible: true,
      title: label,
    });
    lineSeries.setData(points.map((point) => ({ time: point.time, value: point.value })));

    chart.timeScale().fitContent();

    return () => {
      chart.remove();
    };
  }, [label, points]);

  if (points.length === 0) {
    return <div className="empty-state">No fundamentals data returned for this symbol and date range.</div>;
  }
  return <div className="fundamental-chart-canvas" ref={containerRef} aria-label="Fundamentals line chart" />;
}

function DataTable({ rows, columns }: { rows: Array<Record<string, unknown>>; columns: string[] }) {
  if (!rows.length) {
    return <div className="empty-state">No rows to display.</div>;
  }
  const tableColumns = columns.length ? columns : inferColumns(rows);
  return (
    <div className="table-wrap">
      <table>
        <thead><tr>{tableColumns.map((column) => <th key={column}>{column}</th>)}</tr></thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={index}>{tableColumns.map((column) => <td key={column}>{valueText(row[column])}</td>)}</tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return <div className="stat"><span>{label}</span><strong>{value}</strong></div>;
}

function useQueryState<T>(): [QueryState<T>, Dispatch<SetStateAction<QueryState<T>>>] {
  return useState<QueryState<T>>({ loading: false, error: '' });
}

async function runQuery<T>(setState: Dispatch<SetStateAction<QueryState<T>>>, query: () => Promise<T>) {
  setState({ loading: true, error: '' });
  try {
    const data = await query();
    setState({ loading: false, error: '', data });
  } catch (error) {
    setState({ loading: false, error: errorMessage(error) });
  }
}

function inferColumns(rows: Array<Record<string, unknown>>) {
  const seen = new Set<string>();
  for (const row of rows.slice(0, 20)) {
    for (const key of Object.keys(row)) {
      seen.add(key);
    }
  }
  return Array.from(seen).slice(0, 18);
}

function sortBrowserValues(values: BrowserValueListResponse['fields'][number]['values'], sortBy: SymbolSort) {
  const sorted = [...values];
  if (sortBy === 'rows') {
    sorted.sort((left, right) => right.row_count - left.row_count || left.value.localeCompare(right.value));
    return sorted;
  }
  if (sortBy === 'recent') {
    sorted.sort((left, right) => {
      const leftTime = left.last_timestamp ? Date.parse(left.last_timestamp) : 0;
      const rightTime = right.last_timestamp ? Date.parse(right.last_timestamp) : 0;
      return rightTime - leftTime || left.value.localeCompare(right.value);
    });
    return sorted;
  }
  sorted.sort((left, right) => left.value.localeCompare(right.value));
  return sorted;
}

function canOpenMarket(dataset: BrowserDataset, field: string) {
  return field === dataset.symbol_field;
}

function canOpenChain(dataset: BrowserDataset, field: string) {
  if (dataset.market !== 'crypto-options' && dataset.market !== 'us-options') {
    return false;
  }
  return field === dataset.underlying_field || (field === dataset.symbol_field && dataset.symbol_field === dataset.underlying_field);
}

function applySelectionToFilters(dataset: BrowserDataset, selection: BrowserSelection, setSymbol: Dispatch<SetStateAction<string>>, setUnderlying: Dispatch<SetStateAction<string>>) {
  const targetField = selection.field;
  if (dataset.underlying_field && targetField === dataset.underlying_field && dataset.underlying_field !== dataset.symbol_field) {
    setUnderlying(selection.value);
    setSymbol('');
    return;
  }
  setSymbol(selection.value);
  if (dataset.underlying_field !== dataset.symbol_field) {
    setUnderlying('');
  }
}

function browserDatasetToMarket(dataset: BrowserDataset) {
  if (dataset.market === 'crypto-spot') return 'crypto-spot';
  if (dataset.market === 'forex') return 'forex';
  if (dataset.market === 'us-stocks') return 'us-stocks';
  if (dataset.market === 'us-options') return 'us-options';
  return 'crypto-options';
}

function defaultMarketSymbol(dataset: BrowserDataset) {
  if (dataset.market === 'crypto-spot') return 'BTC';
  if (dataset.market === 'forex') return 'EURUSD';
  if (dataset.market === 'us-stocks') return 'AAPL';
  if (dataset.market === 'us-options') return 'SPY';
  return 'BTC';
}

function marketIntervals(market: string) {
  return market === 'forex' ? forexIntervals : defaultIntervals;
}

function defaultMarketInterval(market: string) {
  return market === 'forex' ? '1h' : '1h';
}

function marketQueryKey(query: { market: string; symbol: string; interval: string; from: string; to: string; limit: number }) {
  return [query.market, query.symbol.trim().toUpperCase(), query.interval, query.from, query.to, query.limit].join('|');
}

function upsertBizQueryRows(rows: BizMarketQueryRow[], next: BizMarketQueryRow) {
  const existing = rows.find((row) => row.biz_request_key === next.biz_request_key);
  if (!existing) {
    return [next, ...rows];
  }
  const updated = {
    ...existing,
    ...next,
    first_loaded_at: existing.first_loaded_at,
    cache_status: 'hit',
    hits: existing.hits + 1,
  };
  return [updated, ...rows.filter((row) => row.biz_request_key !== next.biz_request_key)];
}

function toFundamentalChartPoints(points: FundamentalSeriesResponse['data']) {
  return points
    .filter((point) => Number.isFinite(point.value) && !Number.isNaN(Date.parse(point.event_ts)))
    .map((point) => ({
      eventTS: point.event_ts,
      knownAt: point.known_at,
      time: Math.floor(Date.parse(point.event_ts) / 1000) as UTCTimestamp,
      value: point.value,
      filled: point.filled ?? false,
      source: point.source,
      revision: point.revision,
    }));
}

function fundamentalFactorLabel(entry: FundamentalFactorCatalogEntry) {
  return entry.unit ? `${entry.display_name} (${entry.factor_code}, ${entry.unit})` : `${entry.display_name} (${entry.factor_code})`;
}

function formatFundamentalValue(value: number, factorEntry?: FundamentalFactorCatalogEntry) {
  if (!Number.isFinite(value)) {
    return 'n/a';
  }
  const maximumFractionDigits = factorEntry?.unit === '%' ? 2 : value >= 100 ? 1 : 2;
  const minimumFractionDigits = factorEntry?.unit === '%' || value < 100 ? 2 : 1;
  const formatted = new Intl.NumberFormat('en-US', { maximumFractionDigits, minimumFractionDigits }).format(value);
  if (!factorEntry?.unit) {
    return formatted;
  }
  return factorEntry.unit === '%' ? `${formatted}%` : `${formatted} ${factorEntry.unit}`;
}

function toBizDatasetRow(dataset: BrowserDataset, catalogDataset: CatalogDataset | undefined, intervals: string[]) {
  return {
    biz_table: `biz_${dataset.name}`,
    market: browserDatasetToMarket(dataset),
    relation: dataset.relation,
    status: catalogDataset?.status ?? 'unknown',
    freshness: catalogDataset?.freshness ?? 'unknown',
    row_count: formatNumber(catalogDataset?.row_count ?? 0),
    last_timestamp: catalogDataset?.last_timestamp ?? 'n/a',
    intervals: intervals.join(', '),
  };
}

function defaultDateWindow(lastTimestamp?: string): DateWindow {
  if (!lastTimestamp) {
    return fallbackDateWindow;
  }
  const lastDate = new Date(lastTimestamp);
  if (Number.isNaN(lastDate.getTime())) {
    return fallbackDateWindow;
  }
  const toDate = new Date(lastDate.getTime() + 1000 * 60 * 60 * 24);
  const fromDate = new Date(toDate.getTime() - 1000 * 60 * 60 * 24 * 30);
  return { from: toDateInput(fromDate), to: toDateInput(toDate) };
}

function readBizBootstrapCache() {
  try {
    const raw = localStorage.getItem(bizBootstrapCacheKey);
    if (!raw) return undefined;
    const cache = JSON.parse(raw) as BizBootstrapCache;
    if (Date.now() - cache.cachedAt > bizBootstrapCacheMaxAgeMs) {
      localStorage.removeItem(bizBootstrapCacheKey);
      return undefined;
    }
    return cache;
  } catch {
    localStorage.removeItem(bizBootstrapCacheKey);
    return undefined;
  }
}

function writeBizBootstrapCache(cache: BizBootstrapCache) {
  try {
    localStorage.setItem(bizBootstrapCacheKey, JSON.stringify(cache));
  } catch {
    // Ignore quota errors; the API request cache still handles de-duplication.
  }
}

function clearBizBootstrapCache() {
  localStorage.removeItem(bizBootstrapCacheKey);
}

async function resolveDefaultMarketSymbol(dataset: BrowserDataset, dateWindow: DateWindow) {
  if (dataset.market === 'crypto-spot' || dataset.market === 'us-stocks') {
    return defaultMarketSymbol(dataset);
  }

  if (dataset.market === 'us-options') {
    const preview = await api.preview(dataset.name, {
      from: dateWindow.from,
      to: dateWindow.to,
      limit: 1,
      underlying: 'SPY',
      columns: 'symbol',
    });
    const value = preview.data[0]?.symbol;
    return typeof value === 'string' && value.trim() ? value : defaultMarketSymbol(dataset);
  }

  if (dataset.market === 'crypto-options') {
    const preview = await api.preview(dataset.name, {
      from: dateWindow.from,
      to: dateWindow.to,
      limit: 1,
      underlying: 'BTC',
      columns: 'symbol_id,base_asset',
    });
    const rawID = preview.data[0]?.symbol_id;
    const baseAsset = preview.data[0]?.base_asset;
    if (rawID === undefined || rawID === null || !`${rawID}`.trim()) {
      return defaultMarketSymbol(dataset);
    }
    let symbolID: bigint;
    try {
      symbolID = BigInt(`${rawID}`.trim());
    } catch {
      return defaultMarketSymbol(dataset);
    }
    const cursor = encodeRawURLBase64((symbolID - 1n).toString());
    const symbols = await api.cryptoOptionSymbols({ base_asset: typeof baseAsset === 'string' && baseAsset.trim() ? baseAsset : 'BTC', cursor, limit: 1 });
    return symbols.data[0]?.symbol ?? defaultMarketSymbol(dataset);
  }

  return defaultMarketSymbol(dataset);
}

function encodeRawURLBase64(value: string) {
  return btoa(value).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function marketSymbolValue(row: MarketSymbolRow) {
  if (typeof row.symbol === 'string' && row.symbol.trim()) return row.symbol;
  if (row.symbol_id !== undefined && row.symbol_id !== null) return String(row.symbol_id);
  return '';
}

function marketSymbolLabel(row: MarketSymbolRow) {
  const value = marketSymbolValue(row);
  const details = [row.underlying, row.base_asset, row.option_type, row.expiration ? shortDate(row.expiration) : '', row.strike ?? row.strike_price, row.profile?.sector]
    .filter((item) => item !== undefined && item !== null && `${item}`.trim() !== '')
    .join(' / ');
  return details ? `${value} - ${details}` : value;
}

function toChartBars(rows: BarRow[]) {
  const byTime = new Map<number, ChartBar>();
  for (const row of rows) {
    const time = toChartTime(row.timestamp);
    const open = firstFinite(row.open, row.mark_open, row.last_open, row.underlying_price_open);
    const high = firstFinite(row.high, row.mark_high, row.last_high, row.underlying_price_high);
    const low = firstFinite(row.low, row.mark_low, row.last_low, row.underlying_price_low);
    const close = firstFinite(row.close, row.mark_close, row.last_close, row.underlying_close, row.underlying_price_close);
    if (!time || open === undefined || high === undefined || low === undefined || close === undefined) {
      continue;
    }
    if (open <= 0 || high <= 0 || low <= 0 || close <= 0) {
      continue;
    }
    byTime.set(time, {
      timestamp: row.timestamp,
      time: time as UTCTimestamp,
      open,
      high: Math.max(high, open, close),
      low: Math.min(low, open, close),
      close,
      volume: firstFinite(row.volume, row.transactions, row.tick_count) ?? 0,
      raw: row,
    });
  }
  return Array.from(byTime.values()).sort((left, right) => left.time - right.time);
}

function toGapRows(bars: ChartBar[], interval: string): BizMarketGapRow[] {
  const expectedSeconds = intervalSeconds(interval);
  if (!expectedSeconds || bars.length < 2) return [];
  const rows: BizMarketGapRow[] = [];
  for (let index = 1; index < bars.length; index += 1) {
    const previous = bars[index - 1];
    const current = bars[index];
    const deltaSeconds = Number(current.time) - Number(previous.time);
    if (deltaSeconds <= expectedSeconds * 1.5) {
      continue;
    }
    rows.push({
      biz_gap: `biz_gap_${rows.length + 1}`,
      from: previous.timestamp,
      to: current.timestamp,
      missing_span: formatDuration(deltaSeconds),
    });
  }
  return rows;
}

function intervalSeconds(interval: string) {
  const match = interval.match(/^(\d+)(m|h|d)$/);
  if (!match) return 0;
  const amount = Number(match[1]);
  if (match[2] === 'm') return amount * 60;
  if (match[2] === 'h') return amount * 60 * 60;
  return amount * 24 * 60 * 60;
}

function formatDuration(seconds: number) {
  const days = seconds / 86400;
  if (days >= 1) return `${Math.round(days * 10) / 10}d`;
  const hours = seconds / 3600;
  if (hours >= 1) return `${Math.round(hours * 10) / 10}h`;
  return `${Math.round(seconds / 60)}m`;
}

function toChartTime(timestamp: string) {
  const parsed = Date.parse(timestamp);
  if (!Number.isFinite(parsed)) return 0;
  return Math.floor(parsed / 1000);
}

function firstFinite(...values: unknown[]) {
  for (const value of values) {
    const numberValue = Number(value);
    if (Number.isFinite(numberValue)) {
      return numberValue;
    }
  }
  return undefined;
}

function toCandlestickData(bar: ChartBar): CandlestickData<UTCTimestamp> {
  return {
    time: bar.time,
    open: bar.open,
    high: bar.high,
    low: bar.low,
    close: bar.close,
  };
}

function toVolumeData(bar: ChartBar): HistogramData<UTCTimestamp> {
  return {
    time: bar.time,
    value: bar.volume,
    color: bar.close >= bar.open ? 'rgba(31, 138, 91, 0.35)' : 'rgba(196, 69, 54, 0.35)',
  };
}

function formatPrice(value: number) {
  if (Math.abs(value) >= 1000) return new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 }).format(value);
  if (Math.abs(value) >= 1) return new Intl.NumberFormat('en-US', { maximumFractionDigits: 4 }).format(value);
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 8 }).format(value);
}

function formatCompactNumber(value: number) {
  return new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 2 }).format(value);
}

function shortDate(value: string) {
  if (!value) return '';
  return value.slice(0, 10);
}

function compactDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return shortDate(value);
  return `${String(date.getUTCMonth() + 1).padStart(2, '0')}/${String(date.getUTCDate()).padStart(2, '0')}/${date.getUTCFullYear()}`;
}

function toDateInput(date: Date) {
  return date.toISOString().slice(0, 10);
}

function valueText(value: unknown) {
  if (value === null || value === undefined) return '';
  if (typeof value === 'number') return formatTableNumber(value);
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

function formatTableNumber(value: number) {
  if (!Number.isFinite(value)) return '';
  if (Number.isInteger(value)) return formatNumber(value);
  if (Math.abs(value) >= 1000) return new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 }).format(value);
  return String(Math.round(value * 1000000) / 1000000);
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value);
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
