import { useEffect, useMemo, useState } from 'react';
import type { Dispatch, ReactNode, SetStateAction } from 'react';
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

type CatalogDataset = DatasetCatalogResponse['datasets'][number];

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
  const [apiKey, setApiKey] = useState(getStoredApiKey());
  const [status, setStatus] = useState<'checking' | 'ready' | 'error'>('checking');
  const [statusError, setStatusError] = useState('');
  const [datasets, setDatasets] = useState<BrowserDataset[]>([]);
  const [catalog, setCatalog] = useState<DatasetCatalogResponse>();
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
    setStatus('checking');
    setStatusError('');
    try {
      const [ready, presets, catalogResp] = await Promise.all([api.ready(), api.presets(), api.datasets()]);
      setStatus(ready.status === 'ready' ? 'ready' : 'error');
      setDatasets(presets.datasets);
      setCatalog(catalogResp);
    } catch (error) {
      setStatus('error');
      setStatusError(errorMessage(error));
    }
  }

  function saveApiKey() {
    setStoredApiKey(apiKey);
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
            <button className="icon-button" onClick={() => setRefreshToken((value) => value + 1)} title="Refresh" aria-label="Refresh">
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
  if (tab === 'market') return <MarketPanel key={panelKey} dataset={dataset} dateWindow={dateWindow} selection={selection} />;
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

function MarketPanel({ dataset, dateWindow, selection }: { dataset: BrowserDataset; dateWindow: DateWindow; selection?: BrowserSelection }) {
  const [symbol, setSymbol] = useState(defaultMarketSymbol(dataset));
  const [interval, setInterval] = useState('1h');
  const [from, setFrom] = useState(dateWindow.from);
  const [to, setTo] = useState(dateWindow.to);
  const [state, setState] = useQueryState<{ market: string; response: BarResponse }>();

  useEffect(() => {
    setSymbol(defaultMarketSymbol(dataset));
  }, [dataset.name]);

  useEffect(() => {
    let cancelled = false;
    resolveDefaultMarketSymbol(dataset, dateWindow)
      .then((nextSymbol) => {
        if (!cancelled && nextSymbol) {
          setSymbol(nextSymbol);
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
  }, [selection]);

  const market = browserDatasetToMarket(dataset);
  return (
    <Panel title="Market Series" icon={<Activity size={16} />} loading={state.loading} error={state.error} action={<RunButton onClick={() => runQuery(setState, async () => ({ market, response: await api.bars(market, { symbol, interval, from, to, limit: 1000 }) }))} />}>
      <QueryControls>
        <Control label="Market"><input value={market} readOnly /></Control>
        <Control label="Symbol"><input value={symbol} onChange={(event) => setSymbol(event.target.value)} /></Control>
        <Control label="Interval"><input value={interval} onChange={(event) => setInterval(event.target.value)} /></Control>
        <Control label="From"><input value={from} onChange={(event) => setFrom(event.target.value)} type="date" /></Control>
        <Control label="To"><input value={to} onChange={(event) => setTo(event.target.value)} type="date" /></Control>
      </QueryControls>
      {state.data && <SeriesView rows={state.data.response.data} />}
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
        {loading && <div className="empty-state">Loading...</div>}
        {error && <div className="error-state">{error}</div>}
        {!loading && !error && children}
      </div>
    </div>
  );
}

function RunButton({ onClick }: { onClick: () => void }) {
  return <button className="primary-button" onClick={onClick}><Play size={15} />Run</button>;
}

function QueryControls({ children }: { children: ReactNode }) {
  return <div className="controls">{children}</div>;
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

function SeriesView({ rows }: { rows: BarRow[] }) {
  const points = rows.map((row) => ({ timestamp: row.timestamp, value: Number(row.close ?? row.mark_close ?? row.last_close ?? row.underlying_close ?? 0) })).filter((point) => Number.isFinite(point.value) && point.value > 0);
  return (
    <>
      <div className="chart"><LineChart points={points} /></div>
      <DataTable rows={rows.slice(0, 200) as Array<Record<string, unknown>>} columns={inferColumns(rows)} />
    </>
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

function LineChart({ points }: { points: Array<{ timestamp: string; value: number }> }) {
  if (points.length < 2) {
    return <div className="empty-state">Run a query with at least two price points.</div>;
  }
  const width = 900;
  const height = 240;
  const padding = 18;
  const min = Math.min(...points.map((point) => point.value));
  const max = Math.max(...points.map((point) => point.value));
  const spread = max - min || 1;
  const coords = points.map((point, index) => {
    const x = padding + (index / (points.length - 1)) * (width - padding * 2);
    const y = height - padding - ((point.value - min) / spread) * (height - padding * 2);
    return `${x},${y}`;
  });
  const area = `${padding},${height - padding} ${coords.join(' ')} ${width - padding},${height - padding}`;
  return (
    <svg className="line-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Price series chart">
      <polyline className="chart-area" points={area} />
      <polyline className="chart-line" points={coords.join(' ')} />
    </svg>
  );
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
  if (dataset.market === 'us-stocks') return 'us-stocks';
  if (dataset.market === 'us-options') return 'us-options';
  return 'crypto-options';
}

function defaultMarketSymbol(dataset: BrowserDataset) {
  if (dataset.market === 'crypto-spot') return 'BTC';
  if (dataset.market === 'us-stocks') return 'AAPL';
  if (dataset.market === 'us-options') return 'SPY';
  return 'BTC';
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
    if (typeof rawID !== 'string' || !rawID.trim()) {
      return defaultMarketSymbol(dataset);
    }
    const symbolID = BigInt(rawID);
    const cursor = encodeRawURLBase64((symbolID - 1n).toString());
    const symbols = await api.cryptoOptionSymbols({ base_asset: typeof baseAsset === 'string' && baseAsset.trim() ? baseAsset : 'BTC', cursor, limit: 1 });
    return symbols.data[0]?.symbol ?? defaultMarketSymbol(dataset);
  }

  return defaultMarketSymbol(dataset);
}

function encodeRawURLBase64(value: string) {
  return btoa(value).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function toDateInput(date: Date) {
  return date.toISOString().slice(0, 10);
}

function valueText(value: unknown) {
  if (value === null || value === undefined) return '';
  if (typeof value === 'number') return Number.isFinite(value) ? String(Math.round(value * 1000000) / 1000000) : '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value);
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
