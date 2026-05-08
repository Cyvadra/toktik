package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	browserDefaultLimit      = 100
	browserMaxLimit          = 1000
	browserValueDefaultLimit = 500
	browserValueMaxLimit     = 5000
	browserValueCacheTTL     = 5 * time.Minute
)

var browserIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type browserDatasetSpec struct {
	Name            string
	Market          string
	Relation        string
	TimeField       string
	SymbolField     string
	UnderlyingField string
	DefaultColumns  []string
	Checks          map[string]string
}

type DataBrowserService struct {
	repo     *chrepo.Repo
	datasets map[string]browserDatasetSpec
	ordered  []string
	cache    cache.Store
}

func NewDataBrowserService(repo *chrepo.Repo) *DataBrowserService {
	datasets := map[string]browserDatasetSpec{}
	for _, spec := range defaultBrowserDatasets() {
		datasets[spec.Name] = spec
	}
	ordered := make([]string, 0, len(datasets))
	for name := range datasets {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	return &DataBrowserService{repo: repo, datasets: datasets, ordered: ordered, cache: cache.NewMemoryStore()}
}

func (s *DataBrowserService) ListBrowserPresets(_ context.Context) (*dto.BrowserPresetResponse, error) {
	resp := &dto.BrowserPresetResponse{Datasets: make([]dto.BrowserDatasetDescriptor, 0, len(s.ordered))}
	for _, name := range s.ordered {
		resp.Datasets = append(resp.Datasets, describeBrowserDataset(s.datasets[name]))
	}
	return resp, nil
}

func (s *DataBrowserService) QueryDatasetSchema(ctx context.Context, req dto.BrowserSchemaRequest) (*dto.BrowserSchemaResponse, error) {
	spec, err := s.dataset(req.Dataset)
	if err != nil {
		return nil, err
	}
	columns, err := s.columns(ctx, spec)
	if err != nil {
		return nil, err
	}
	return &dto.BrowserSchemaResponse{Dataset: describeBrowserDataset(spec), Columns: columns}, nil
}

func (s *DataBrowserService) QueryDatasetPreview(ctx context.Context, req dto.BrowserPreviewRequest) (*dto.BrowserPreviewResponse, error) {
	spec, err := s.dataset(req.Dataset)
	if err != nil {
		return nil, err
	}
	columns, err := s.columns(ctx, spec)
	if err != nil {
		return nil, err
	}
	columnMap := browserColumnMap(columns)
	selected, err := selectBrowserColumns(req.Columns, spec.DefaultColumns, columnMap)
	if err != nil {
		return nil, err
	}
	limit := normalizeBrowserLimit(req.Limit)
	whereSQL, args, err := browserWhere(spec, req.Symbol, req.Underlying, req.From, req.To)
	if err != nil {
		return nil, err
	}
	orderSQL := ""
	if spec.TimeField != "" {
		orderSQL = " ORDER BY " + quoteBrowserIdent(spec.TimeField) + " DESC"
	}
	query := chquery.RelationPreview(quoteBrowserIdent(spec.Relation), quoteBrowserIdents(selected), whereSQL, orderSQL, limit)
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		values := make([]string, len(selected))
		scan := make([]any, len(selected))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(selected))
		for i, column := range selected {
			row[column] = normalizeBrowserValue(values[i])
		}
		data = append(data, row)
	}
	return &dto.BrowserPreviewResponse{Dataset: describeBrowserDataset(spec), Columns: selected, Data: data}, rows.Err()
}

func (s *DataBrowserService) QueryDatasetCoverage(ctx context.Context, req dto.BrowserCoverageRequest) (*dto.BrowserCoverageResponse, error) {
	spec, err := s.dataset(req.Dataset)
	if err != nil {
		return nil, err
	}
	if spec.TimeField == "" {
		return nil, dto.NewValidationError("dataset %q has no time field", spec.Name)
	}
	whereSQL, args, err := browserWhere(spec, req.Symbol, req.Underlying, req.From, req.To)
	if err != nil {
		return nil, err
	}
	query := chquery.RelationCoverageSummary(quoteBrowserIdent(spec.Relation), quoteBrowserIdent(spec.TimeField), whereSQL)
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resp := &dto.BrowserCoverageResponse{Dataset: describeBrowserDataset(spec)}
	if rows.Next() {
		var firstTS, lastTS time.Time
		if err := rows.Scan(&resp.RowCount, &firstTS, &lastTS); err != nil {
			return nil, err
		}
		if !firstTS.IsZero() && firstTS.UTC().Unix() != 0 {
			first := firstTS.UTC()
			resp.FirstTimestamp = &first
		}
		if !lastTS.IsZero() && lastTS.UTC().Unix() != 0 {
			last := lastTS.UTC()
			resp.LastTimestamp = &last
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dailyRows, err := s.repo.Query(ctx, chquery.RelationDailyCoverage(quoteBrowserIdent(spec.Relation), quoteBrowserIdent(spec.TimeField), whereSQL), args...)
	if err != nil {
		return nil, err
	}
	defer dailyRows.Close()
	for dailyRows.Next() {
		var row dto.BrowserDailyCoverage
		if err := dailyRows.Scan(&row.Date, &row.RowCount); err != nil {
			return nil, err
		}
		resp.Daily = append(resp.Daily, row)
	}
	return resp, dailyRows.Err()
}

func (s *DataBrowserService) QueryFieldProfile(ctx context.Context, req dto.BrowserFieldProfileRequest) (*dto.BrowserFieldProfileResponse, error) {
	spec, err := s.dataset(req.Dataset)
	if err != nil {
		return nil, err
	}
	columns, err := s.columns(ctx, spec)
	if err != nil {
		return nil, err
	}
	columnMap := browserColumnMap(columns)
	column, ok := columnMap[req.Field]
	if !ok {
		return nil, dto.NewValidationError("unknown field %q for dataset %q", req.Field, spec.Name)
	}
	whereSQL, args, err := browserWhere(spec, "", "", req.From, req.To)
	if err != nil {
		return nil, err
	}
	query := chquery.RelationFieldProfile(quoteBrowserIdent(spec.Relation), quoteBrowserIdent(req.Field), column.Type, whereSQL)
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resp := &dto.BrowserFieldProfileResponse{Dataset: describeBrowserDataset(spec), Field: req.Field, Type: column.Type}
	if rows.Next() {
		var zeroCount, emptyCount uint64
		var minValue, maxValue any
		if err := rows.Scan(&resp.RowCount, &resp.NullCount, &zeroCount, &emptyCount, &resp.DistinctCount, &minValue, &maxValue); err != nil {
			return nil, err
		}
		if browserIsNumeric(column.Type) {
			resp.ZeroCount = &zeroCount
		}
		if browserIsString(column.Type) {
			resp.EmptyCount = &emptyCount
		}
		resp.Min = normalizeBrowserValue(minValue)
		resp.Max = normalizeBrowserValue(maxValue)
	}
	return resp, rows.Err()
}

func (s *DataBrowserService) QueryValidCount(ctx context.Context, req dto.BrowserValidCountRequest) (*dto.BrowserValidCountResponse, error) {
	spec, err := s.dataset(req.Dataset)
	if err != nil {
		return nil, err
	}
	check := strings.TrimSpace(req.Check)
	if check == "" {
		check = "default"
	}
	validExpr, ok := spec.Checks[check]
	if !ok {
		return nil, dto.NewValidationError("unknown check %q for dataset %q", check, spec.Name)
	}
	whereSQL, args, err := browserWhere(spec, "", "", req.From, req.To)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Query(ctx, chquery.RelationValidCount(quoteBrowserIdent(spec.Relation), validExpr, whereSQL), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resp := &dto.BrowserValidCountResponse{Dataset: describeBrowserDataset(spec), Check: check}
	if rows.Next() {
		if err := rows.Scan(&resp.RowCount, &resp.ValidCount); err != nil {
			return nil, err
		}
		resp.InvalidCount = resp.RowCount - resp.ValidCount
	}
	return resp, rows.Err()
}

func (s *DataBrowserService) QueryDatasetValues(ctx context.Context, req dto.BrowserValueListRequest) (*dto.BrowserValueListResponse, error) {
	spec, err := s.dataset(req.Dataset)
	if err != nil {
		return nil, err
	}
	fields := browserValueFields(spec)
	resp := &dto.BrowserValueListResponse{Dataset: describeBrowserDataset(spec), Fields: make([]dto.BrowserValueFieldList, 0, len(fields))}
	if len(fields) == 0 {
		return resp, nil
	}

	limit := normalizeBrowserValueLimit(req.Limit)
	search := strings.TrimSpace(req.Search)
	cacheKey := browserValuesCacheKey(spec.Name, search, limit)
	if s.cache != nil {
		if cachedValue, ok, err := s.cache.Get(ctx, cacheKey); err == nil && ok {
			if err := json.Unmarshal(cachedValue, resp); err == nil {
				resp.Cached = true
				return resp, nil
			}
		}
	}

	columns, err := s.columns(ctx, spec)
	if err != nil {
		return nil, err
	}
	columnMap := browserColumnMap(columns)
	for _, field := range fields {
		if _, ok := columnMap[field]; !ok {
			return nil, dto.NewValidationError("unknown value field %q for dataset %q", field, spec.Name)
		}
		items, err := s.queryBrowserFieldValues(ctx, spec, field, search, limit)
		if err != nil {
			return nil, err
		}
		resp.Fields = append(resp.Fields, dto.BrowserValueFieldList{Field: field, Values: items})
	}

	if s.cache != nil {
		if payload, err := json.Marshal(resp); err == nil {
			_ = s.cache.Set(ctx, cacheKey, payload, browserValueCacheTTL)
		}
	}
	return resp, nil
}

func (s *DataBrowserService) columns(ctx context.Context, spec browserDatasetSpec) ([]dto.BrowserColumn, error) {
	rows, err := s.repo.Query(ctx, chquery.RelationColumns, clickhouse.Named("relation", spec.Relation))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []dto.BrowserColumn{}
	for rows.Next() {
		var column dto.BrowserColumn
		if err := rows.Scan(&column.Name, &column.Type, &column.Position, &column.DefaultKind, &column.DefaultExpression, &column.Comment, &column.CodecExpression); err != nil {
			return nil, err
		}
		column.IsNullable = strings.HasPrefix(column.Type, "Nullable(")
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, dto.NewValidationError("dataset %q relation %q has no visible columns", spec.Name, spec.Relation)
	}
	return columns, nil
}

func (s *DataBrowserService) dataset(name string) (browserDatasetSpec, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	spec, ok := s.datasets[name]
	if !ok {
		return browserDatasetSpec{}, dto.NewValidationError("unknown browser dataset %q", name)
	}
	return spec, nil
}

func (s *DataBrowserService) queryBrowserFieldValues(ctx context.Context, spec browserDatasetSpec, field, search string, limit int) ([]dto.BrowserValueItem, error) {
	searchSQL := ""
	args := []any{}
	if search != "" {
		searchSQL = fmt.Sprintf(" AND CAST(%s, 'String') ILIKE {search:String}", quoteBrowserIdent(field))
		args = append(args, clickhouse.Named("search", "%"+search+"%"))
	}
	query := chquery.RelationFieldValues(quoteBrowserIdent(spec.Relation), quoteBrowserIdent(field), quoteBrowserIdent(spec.TimeField), searchSQL, limit)
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]dto.BrowserValueItem, 0, limit)
	for rows.Next() {
		var item dto.BrowserValueItem
		var lastTS time.Time
		if err := rows.Scan(&item.Value, &item.RowCount, &lastTS); err != nil {
			return nil, err
		}
		if !lastTS.IsZero() && lastTS.UTC().Unix() != 0 {
			last := lastTS.UTC()
			item.LastTimestamp = &last
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func defaultBrowserDatasets() []browserDatasetSpec {
	return []browserDatasetSpec{
		{Name: "crypto-options-bars", Market: "crypto-options", Relation: chquery.CryptoOptionsBar1m, TimeField: "timestamp", SymbolField: "base_asset", UnderlyingField: "base_asset", DefaultColumns: []string{"timestamp", "symbol_id", "base_asset", "mark_close", "bid_close", "ask_close", "mark_iv_close", "delta", "gamma", "volume", "open_interest"}, Checks: map[string]string{"default": "timestamp > toDateTime(0, 'UTC') AND mark_close > 0 AND bid_close >= 0 AND ask_close >= 0 AND volume >= 0 AND open_interest >= 0"}},
		{Name: "crypto-spot-bars", Market: "crypto-spot", Relation: chquery.CryptoSpotBar1m, TimeField: "timestamp", SymbolField: "symbol", DefaultColumns: []string{"timestamp", "symbol", "open", "high", "low", "close", "volume", "tick_count"}, Checks: map[string]string{"default": "timestamp > toDateTime(0, 'UTC') AND open > 0 AND high > 0 AND low > 0 AND close > 0 AND volume >= 0"}},
		{Name: "forex-bars", Market: "forex", Relation: chquery.ForexBar1m, TimeField: "timestamp", SymbolField: "symbol", DefaultColumns: []string{"timestamp", "symbol", "open", "high", "low", "close", "volume", "transactions"}, Checks: map[string]string{"default": "timestamp > toDateTime(0, 'UTC') AND open > 0 AND high > 0 AND low > 0 AND close > 0 AND volume >= 0"}},
		{Name: "us-stocks-bars", Market: "us-stocks", Relation: chquery.USStocksBar1m, TimeField: "timestamp", SymbolField: "symbol", DefaultColumns: []string{"timestamp", "symbol", "open", "high", "low", "close", "volume", "transactions"}, Checks: map[string]string{"default": "timestamp > toDateTime(0, 'UTC') AND open > 0 AND high > 0 AND low > 0 AND close > 0 AND volume >= 0"}},
		{Name: "us-options-bars", Market: "us-options", Relation: chquery.USOptionsBar1m, TimeField: "timestamp", SymbolField: "symbol", UnderlyingField: "underlying", DefaultColumns: []string{"timestamp", "symbol", "underlying", "option_type", "expiration", "strike", "close", "underlying_close", "implied_volatility", "delta", "gamma", "volume", "transactions"}, Checks: map[string]string{"default": "timestamp > toDateTime(0, 'UTC') AND close >= 0 AND underlying_close > 0 AND implied_volatility >= 0 AND volume >= 0"}},
		{Name: "us-options-chain", Market: "us-options", Relation: chquery.USOptionsChain1d, TimeField: "timestamp", SymbolField: "symbol", UnderlyingField: "underlying", DefaultColumns: []string{"timestamp", "symbol", "underlying", "option_type", "expiration", "strike", "close", "underlying_close", "implied_volatility", "delta", "volume", "transactions"}, Checks: map[string]string{"default": "timestamp > toDateTime(0, 'UTC') AND close >= 0 AND underlying_close > 0 AND implied_volatility >= 0"}},
		{Name: "feature-volatility-snapshots", Market: "feature-store", Relation: chquery.FeatureVolatilitySnapshotDaily, TimeField: "updated_at", SymbolField: "underlying", UnderlyingField: "underlying", DefaultColumns: []string{"updated_at", "market", "underlying", "as_of_date", "price_observations", "iv_observations", "hv10", "hv20", "hv30", "current_iv", "iv_percentile", "iv_rank"}, Checks: map[string]string{"default": "updated_at > toDateTime(0, 'UTC') AND price_observations >= 0 AND iv_observations >= 0"}},
		{Name: "feature-daily-panels", Market: "feature-store", Relation: chquery.FeatureDailyPanelDaily, TimeField: "updated_at", SymbolField: "underlying", UnderlyingField: "underlying", DefaultColumns: []string{"updated_at", "market", "underlying", "as_of_date", "price_observations", "iv_observations", "hv20", "current_iv", "iv_rank", "liquidity_contract_count", "liquidity_active_contract_count"}, Checks: map[string]string{"default": "updated_at > toDateTime(0, 'UTC') AND price_observations >= 0 AND iv_observations >= 0"}},
	}
}

func describeBrowserDataset(spec browserDatasetSpec) dto.BrowserDatasetDescriptor {
	checks := make([]string, 0, len(spec.Checks))
	for name := range spec.Checks {
		checks = append(checks, name)
	}
	sort.Strings(checks)
	return dto.BrowserDatasetDescriptor{Name: spec.Name, Market: spec.Market, Relation: spec.Relation, TimeField: spec.TimeField, SymbolField: spec.SymbolField, UnderlyingField: spec.UnderlyingField, Fields: spec.DefaultColumns, Checks: checks}
}

func browserValueFields(spec browserDatasetSpec) []string {
	fields := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, field := range []string{spec.SymbolField, spec.UnderlyingField} {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	return fields
}

func browserValuesCacheKey(dataset, search string, limit int) string {
	return fmt.Sprintf("browser:values:v1:%s:%d:%s", strings.ToLower(strings.TrimSpace(dataset)), limit, strings.ToLower(strings.TrimSpace(search)))
}

func browserWhere(spec browserDatasetSpec, symbol, underlying, from, to string) (string, []any, error) {
	clauses := []string{}
	args := []any{}
	if symbol = strings.TrimSpace(symbol); symbol != "" {
		if spec.SymbolField == "" {
			return "", nil, dto.NewValidationError("dataset %q does not support symbol filtering", spec.Name)
		}
		clauses = append(clauses, fmt.Sprintf("%s = {symbol:String}", quoteBrowserIdent(spec.SymbolField)))
		args = append(args, clickhouse.Named("symbol", symbol))
	}
	if underlying = strings.TrimSpace(underlying); underlying != "" {
		field := spec.UnderlyingField
		if field == "" {
			field = spec.SymbolField
		}
		if field == "" {
			return "", nil, dto.NewValidationError("dataset %q does not support underlying filtering", spec.Name)
		}
		clauses = append(clauses, fmt.Sprintf("%s = {underlying:String}", quoteBrowserIdent(field)))
		args = append(args, clickhouse.Named("underlying", underlying))
	}
	if strings.TrimSpace(from) != "" || strings.TrimSpace(to) != "" {
		if spec.TimeField == "" {
			return "", nil, dto.NewValidationError("dataset %q does not support time filtering", spec.Name)
		}
		if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
			return "", nil, dto.NewValidationError("both from and to are required when filtering by time")
		}
		fromT, toT, err := dto.ParseTimeRange(from, to)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, fmt.Sprintf("%s >= toDateTime({from:String}, 'UTC') AND %s < toDateTime({to:String}, 'UTC')", quoteBrowserIdent(spec.TimeField), quoteBrowserIdent(spec.TimeField)))
		args = append(args,
			clickhouse.Named("from", fromT.UTC().Format("2006-01-02 15:04:05")),
			clickhouse.Named("to", toT.UTC().Format("2006-01-02 15:04:05")),
		)
	}
	if len(clauses) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

func selectBrowserColumns(raw string, defaults []string, columnMap map[string]dto.BrowserColumn) ([]string, error) {
	selected := []string{}
	if strings.TrimSpace(raw) == "" {
		selected = append(selected, defaults...)
	} else {
		for _, part := range strings.Split(raw, ",") {
			name := strings.TrimSpace(part)
			if name != "" {
				selected = append(selected, name)
			}
		}
	}
	if len(selected) == 0 {
		return nil, dto.NewValidationError("at least one column is required")
	}
	if len(selected) > 40 {
		return nil, dto.NewValidationError("too many columns requested: max 40")
	}
	for _, name := range selected {
		if _, ok := columnMap[name]; !ok {
			return nil, dto.NewValidationError("unknown column %q", name)
		}
	}
	return selected, nil
}

func normalizeBrowserValueLimit(limit int) int {
	if limit <= 0 {
		return browserValueDefaultLimit
	}
	if limit > browserValueMaxLimit {
		return browserValueMaxLimit
	}
	return limit
}

func browserColumnMap(columns []dto.BrowserColumn) map[string]dto.BrowserColumn {
	columnMap := make(map[string]dto.BrowserColumn, len(columns))
	for _, column := range columns {
		columnMap[column.Name] = column
	}
	return columnMap
}

func normalizeBrowserLimit(limit int) int {
	if limit <= 0 {
		return browserDefaultLimit
	}
	if limit > browserMaxLimit {
		return browserMaxLimit
	}
	return limit
}

func quoteBrowserIdents(names []string) []string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quoteBrowserIdent(name)
	}
	return quoted
}

func quoteBrowserIdent(name string) string {
	if !browserIdentifierRE.MatchString(name) {
		panic("invalid browser identifier: " + name)
	}
	return "`" + name + "`"
}

func normalizeBrowserValue(value any) any {
	switch v := value.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case []byte:
		return string(v)
	default:
		return v
	}
}

func browserIsNumeric(t string) bool {
	return strings.HasPrefix(t, "UInt") || strings.HasPrefix(t, "Int") || strings.HasPrefix(t, "Float") || strings.HasPrefix(t, "Decimal") || (strings.HasPrefix(t, "Nullable(") && browserIsNumeric(strings.TrimSuffix(strings.TrimPrefix(t, "Nullable("), ")")))
}

func browserIsString(t string) bool {
	return t == "String" || t == "LowCardinality(String)" || (strings.HasPrefix(t, "Nullable(") && browserIsString(strings.TrimSuffix(strings.TrimPrefix(t, "Nullable("), ")")))
}
