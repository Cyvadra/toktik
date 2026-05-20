package dataintegrity

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

const (
	TargetUSOptionsAggregates = "us-options-aggregates"
	TargetUSStocksAggregates  = "us-stocks-aggregates"
	TargetOptionChainCache    = "chain-cache"
	TargetFundamentals        = "fundamentals"
	TargetFeatures            = "features"
)

var defaultTargets = []string{
	TargetUSOptionsAggregates,
	TargetUSStocksAggregates,
	TargetOptionChainCache,
	TargetFundamentals,
	TargetFeatures,
}

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Request struct {
	From                          time.Time
	To                            time.Time
	Targets                       []string
	Underlyings                   []string
	Symbols                       []string
	ClickHouseDSN                 string
	FMPAPIKey                     string
	FMPQuarterLimit               int
	FundamentalWorkers            int
	FundamentalBatchSize          int
	FundamentalPageSize           int
	FundamentalQPS                int
	FundamentalLimitSymbols       int
	FundamentalDistributedLimiter usmarket.DistributedRateLimitConfig
	Progress                      func(format string, args ...any)
	Repair                        bool
	DryRun                        bool
	MaxSamples                    int
	LookbackDays                  int
	MinDaysToExpiry               int
	MaxDaysToExpiry               int
	FundamentalStale              time.Duration
	FeatureStale                  time.Duration

	// MaxMemoryUsageBytes caps RAM for individual ClickHouse queries issued
	// by the checker and the repair path (SETTING max_memory_usage). Zero
	// falls back to a safe default in normalizeRequest.
	MaxMemoryUsageBytes uint64
	// MaxBytesBeforeExternalGroupBy spills GROUP BY to disk above this
	// threshold (SETTING max_bytes_before_external_group_by). Zero falls
	// back to a safe default in normalizeRequest.
	MaxBytesBeforeExternalGroupBy uint64
	// MaxThreads caps query parallelism (SETTING max_threads). Zero falls
	// back to a safe default in normalizeRequest.
	MaxThreads int
}

type Report struct {
	From       time.Time      `json:"from"`
	To         time.Time      `json:"to"`
	Targets    []string       `json:"targets"`
	Findings   []Finding      `json:"findings"`
	Repairs    []RepairAction `json:"repairs"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
}

type Finding struct {
	Target           string   `json:"target"`
	Check            string   `json:"check"`
	Severity         Severity `json:"severity"`
	Message          string   `json:"message"`
	Table            string   `json:"table,omitempty"`
	Interval         string   `json:"interval,omitempty"`
	BaseKeys         uint64   `json:"base_keys,omitempty"`
	TargetKeys       uint64   `json:"target_keys,omitempty"`
	MissingKeys      uint64   `json:"missing_keys,omitempty"`
	MissingRatio     float64  `json:"missing_ratio,omitempty"`
	AffectedKeys     uint64   `json:"affected_keys,omitempty"`
	FirstMissingDate string   `json:"first_missing_date,omitempty"`
	LastMissingDate  string   `json:"last_missing_date,omitempty"`
	Samples          []string `json:"samples,omitempty"`
	Offenders        []string `json:"offenders,omitempty"`
	// AffectedMonths lists the YYYYMM partitions (sorted) that contributed
	// MissingKeys > 0 for this finding. Used by the repair path to scope
	// rebuilds to only the partitions that need it.
	AffectedMonths []string `json:"affected_months,omitempty"`
}

type RepairAction struct {
	Target  string `json:"target"`
	Action  string `json:"action"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type Checker struct {
	conn driver.Conn
}

func NewChecker(conn driver.Conn) *Checker {
	return &Checker{conn: conn}
}

func (c *Checker) Run(ctx context.Context, req Request) (Report, error) {
	now := time.Now().UTC()
	req = normalizeRequest(req, now)
	report := Report{From: req.From, To: req.To, Targets: req.Targets, StartedAt: now}
	req.progressf("integrity progress: start window=%s..%s targets=%d", req.From.Format("2006-01-02"), req.To.Format("2006-01-02"), len(req.Targets))

	for index, target := range req.Targets {
		req.progressf("integrity progress: target %d/%d %s started", index+1, len(req.Targets), target)
		switch target {
		case TargetUSOptionsAggregates:
			findings, repairNeeded, err := c.checkAggregateGroup(ctx, req, aggregateGroupRequest{
				Target:     TargetUSOptionsAggregates,
				Check:      "options-1m-vs-aggregate",
				BaseTable:  "us_options_bar_1m",
				KeyColumn:  "underlying",
				FilterKeys: req.Underlyings,
				Intervals:  usmarket.KlineIntervals,
				TableName: func(iv usmarket.KlineInterval) string {
					return "us_options_bar_" + iv.Suffix
				},
			})
			if err != nil {
				return report, err
			}
			report.Findings = append(report.Findings, findings...)
			if repairNeeded {
				report.Repairs = append(report.Repairs, c.repairUSOptionsAggregates(ctx, req, findings)...)
			}
		case TargetUSStocksAggregates:
			findings, repairNeeded, err := c.checkAggregateGroup(ctx, req, aggregateGroupRequest{
				Target:     TargetUSStocksAggregates,
				Check:      "stocks-1m-vs-aggregate",
				BaseTable:  "us_stocks_bar_1m",
				KeyColumn:  "symbol",
				FilterKeys: req.Symbols,
				Intervals:  usmarket.KlineIntervals,
				TableName: func(iv usmarket.KlineInterval) string {
					return "us_stocks_bar_" + iv.Suffix
				},
			})
			if err != nil {
				return report, err
			}
			report.Findings = append(report.Findings, findings...)
			if repairNeeded {
				report.Repairs = append(report.Repairs, c.repairUSStocksAggregates(ctx, req, findings)...)
			}
		case TargetOptionChainCache:
			findings, repairNeeded, err := c.checkAggregateGroup(ctx, req, aggregateGroupRequest{
				Target:     TargetOptionChainCache,
				Check:      "options-1m-vs-chain-cache",
				BaseTable:  "us_options_bar_1m",
				KeyColumn:  "underlying",
				FilterKeys: req.Underlyings,
				Intervals:  usmarket.DefaultChainCacheIntervals,
				TableName: func(iv usmarket.KlineInterval) string {
					return "us_options_chain_" + iv.Suffix
				},
			})
			if err != nil {
				return report, err
			}
			report.Findings = append(report.Findings, findings...)
			if repairNeeded {
				report.Repairs = append(report.Repairs, c.repairOptionChainCache(ctx, req, findings)...)
			}
		case TargetFundamentals:
			findings, err := c.checkFundamentals(ctx, req)
			if err != nil {
				return report, err
			}
			report.Findings = append(report.Findings, findings...)
			if fundamentalsRepairNeeded(findings) {
				report.Repairs = append(report.Repairs, c.repairFundamentals(ctx, req, findings)...)
			}
		case TargetFeatures:
			findings, err := c.checkFeatures(ctx, req)
			if err != nil {
				return report, err
			}
			report.Findings = append(report.Findings, findings...)
			if featureRepairNeeded(findings) {
				report.Repairs = append(report.Repairs, c.repairFeatures(ctx, req, findings)...)
			}
		default:
			return report, fmt.Errorf("unknown integrity target %q", target)
		}
		req.progressf("integrity progress: target %d/%d %s finished", index+1, len(req.Targets), target)
	}

	report.FinishedAt = time.Now().UTC()
	req.progressf("integrity progress: completed findings=%d repairs=%d elapsed=%s", len(report.Findings), len(report.Repairs), report.FinishedAt.Sub(report.StartedAt).Round(time.Second))
	return report, nil
}

func normalizeRequest(req Request, now time.Time) Request {
	if req.To.IsZero() {
		req.To = dateOnly(now)
	} else {
		req.To = dateOnly(req.To)
	}
	if req.From.IsZero() {
		req.From = req.To.AddDate(0, 0, -7)
	} else {
		req.From = dateOnly(req.From)
	}
	if req.MaxSamples <= 0 {
		req.MaxSamples = 10
	}
	if req.FMPQuarterLimit <= 0 {
		req.FMPQuarterLimit = 40
	}
	if req.FundamentalWorkers <= 0 {
		req.FundamentalWorkers = 2
	}
	if req.FundamentalBatchSize <= 0 {
		req.FundamentalBatchSize = 1000
	}
	if req.FundamentalPageSize <= 0 {
		req.FundamentalPageSize = 251
	}
	if req.FundamentalQPS <= 0 {
		req.FundamentalQPS = 5
	}
	if req.LookbackDays <= 0 {
		req.LookbackDays = 252
	}
	if req.MaxDaysToExpiry <= 0 {
		req.MaxDaysToExpiry = 365
	}
	if req.FundamentalStale <= 0 {
		req.FundamentalStale = 120 * 24 * time.Hour
	}
	if req.FeatureStale <= 0 {
		req.FeatureStale = 48 * time.Hour
	}
	if req.MaxMemoryUsageBytes == 0 {
		// 12 GiB. Conservative on a 128 GiB host; prevents a single rebuild
		// query from monopolising RAM and OOM-killing the server.
		req.MaxMemoryUsageBytes = 12 << 30
	}
	if req.MaxBytesBeforeExternalGroupBy == 0 {
		// 8 GiB. Spill GROUP BY to disk above this; required for chain-cache
		// rebuilds whose hash table can otherwise exceed the memory cap.
		req.MaxBytesBeforeExternalGroupBy = 8 << 30
	}
	if req.MaxThreads == 0 {
		req.MaxThreads = 4
	}
	req.Targets = normalizeTargets(req.Targets)
	req.Underlyings = normalizeSymbols(req.Underlyings)
	req.Symbols = normalizeSymbols(req.Symbols)
	return req
}

func (req Request) progressf(format string, args ...any) {
	if req.Progress != nil {
		req.Progress(format, args...)
	}
}

// clickhouseSettingsClause builds a SETTINGS clause that bounds memory and
// thread usage for heavy queries issued by the checker. Returns the empty
// string when no limits are configured.
func (req Request) clickhouseSettingsClause() string {
	return buildClickHouseSettingsClause(req.MaxMemoryUsageBytes, req.MaxBytesBeforeExternalGroupBy, req.MaxThreads)
}

func buildClickHouseSettingsClause(maxMem, externalGroupBy uint64, maxThreads int) string {
	parts := make([]string, 0, 3)
	if maxMem > 0 {
		parts = append(parts, fmt.Sprintf("max_memory_usage = %d", maxMem))
	}
	if externalGroupBy > 0 {
		parts = append(parts, fmt.Sprintf("max_bytes_before_external_group_by = %d", externalGroupBy))
	}
	if maxThreads > 0 {
		parts = append(parts, fmt.Sprintf("max_threads = %d", maxThreads))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\nSETTINGS " + strings.Join(parts, ", ")
}

// affectedMonthsFromFindings collects the union of YYYYMM partitions and the
// distinct interval suffixes reported missing for a given target. The result
// drives the scoped repair path.
func affectedMonthsFromFindings(findings []Finding) (months []string, intervals []string) {
	monthSet := map[string]struct{}{}
	intervalSet := map[string]struct{}{}
	for _, f := range findings {
		if f.MissingKeys == 0 {
			continue
		}
		if f.Interval != "" {
			intervalSet[f.Interval] = struct{}{}
		}
		for _, m := range f.AffectedMonths {
			monthSet[m] = struct{}{}
		}
	}
	months = make([]string, 0, len(monthSet))
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)
	intervals = make([]string, 0, len(intervalSet))
	for s := range intervalSet {
		intervals = append(intervals, s)
	}
	sort.Strings(intervals)
	return months, intervals
}

func normalizeTargets(values []string) []string {
	if len(values) == 0 {
		return append([]string(nil), defaultTargets...)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			target := strings.ToLower(strings.TrimSpace(part))
			if target == "" {
				continue
			}
			if target == "all" {
				return append([]string(nil), defaultTargets...)
			}
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}
			out = append(out, target)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultTargets...)
	}
	return out
}

func normalizeSymbols(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			symbol := strings.ToUpper(strings.TrimSpace(part))
			if symbol == "" {
				continue
			}
			if _, ok := seen[symbol]; ok {
				continue
			}
			seen[symbol] = struct{}{}
			out = append(out, symbol)
		}
	}
	sort.Strings(out)
	return out
}

type aggregateGroupRequest struct {
	Target     string
	Check      string
	BaseTable  string
	KeyColumn  string
	FilterKeys []string
	Intervals  []usmarket.KlineInterval
	TableName  func(usmarket.KlineInterval) string
}

type aggregateCoverageRequest struct {
	Target      string
	Check       string
	Interval    string
	BaseTable   string
	TargetTable string
	KeyColumn   string
	FilterKeys  []string
	From        time.Time
	To          time.Time
	MaxSamples  int
	Settings    string
}

type aggregateChunkStats struct {
	BaseKeys      uint64
	TargetKeys    uint64
	MissingKeys   uint64
	FirstMissing  string
	LastMissing   string
	Samples       []string
	MissingKeySet []string
}

func (c *Checker) checkAggregateGroup(ctx context.Context, req Request, group aggregateGroupRequest) ([]Finding, bool, error) {
	windows := splitMonthlyWindowsInclusive(req.From, req.To)
	findings := make([]Finding, 0, len(group.Intervals))
	repairNeeded := false
	req.progressf("integrity progress: target=%s intervals=%d monthly_chunks=%d", group.Target, len(group.Intervals), len(windows))
	for intervalIndex, interval := range group.Intervals {
		req.progressf("integrity progress: target=%s interval %d/%d %s started", group.Target, intervalIndex+1, len(group.Intervals), interval.Suffix)
		finding := Finding{
			Target:   group.Target,
			Check:    group.Check,
			Severity: SeverityInfo,
			Table:    group.TableName(interval),
			Interval: interval.Suffix,
			Message:  fmt.Sprintf("%s coverage ok for %s", group.TableName(interval), interval.Suffix),
		}
		missingKeySet := map[string]struct{}{}
		monthSet := map[string]struct{}{}
		for windowIndex, window := range windows {
			req.progressf("integrity progress: target=%s interval=%s chunk %d/%d %s..%s", group.Target, interval.Suffix, windowIndex+1, len(windows), window.From.Format("2006-01-02"), window.To.Format("2006-01-02"))
			chunk, err := c.checkAggregateCoverageChunk(ctx, aggregateCoverageRequest{
				Target:      group.Target,
				Check:       group.Check,
				Interval:    interval.Suffix,
				BaseTable:   group.BaseTable,
				TargetTable: group.TableName(interval),
				KeyColumn:   group.KeyColumn,
				FilterKeys:  group.FilterKeys,
				From:        window.From,
				To:          window.To,
				MaxSamples:  req.MaxSamples,
				Settings:    req.clickhouseSettingsClause(),
			})
			if err != nil {
				return nil, false, err
			}
			finding.BaseKeys += chunk.BaseKeys
			finding.TargetKeys += chunk.TargetKeys
			finding.MissingKeys += chunk.MissingKeys
			mergeMissingWindow(&finding, chunk.FirstMissing, chunk.LastMissing)
			appendSamples(&finding.Samples, chunk.Samples, req.MaxSamples)
			for _, key := range chunk.MissingKeySet {
				missingKeySet[key] = struct{}{}
			}
			if chunk.MissingKeys > 0 {
				monthSet[window.From.Format("200601")] = struct{}{}
			}
		}
		finding.AffectedKeys = uint64(len(missingKeySet))
		if len(monthSet) > 0 {
			months := make([]string, 0, len(monthSet))
			for m := range monthSet {
				months = append(months, m)
			}
			sort.Strings(months)
			finding.AffectedMonths = months
		}
		if finding.BaseKeys > 0 && finding.MissingKeys > 0 {
			finding.MissingRatio = float64(finding.MissingKeys) / float64(finding.BaseKeys)
			finding.Severity = SeverityCritical
			finding.Message = fmt.Sprintf("%s is missing %d date/key aggregate rows from %d base date/key pairs", finding.Table, finding.MissingKeys, finding.BaseKeys)
			repairNeeded = true
		}
		req.progressf("integrity progress: target=%s interval %d/%d %s finished base=%d missing=%d", group.Target, intervalIndex+1, len(group.Intervals), interval.Suffix, finding.BaseKeys, finding.MissingKeys)
		findings = append(findings, finding)
	}
	return findings, repairNeeded, nil
}

func (c *Checker) checkAggregateCoverageChunk(ctx context.Context, req aggregateCoverageRequest) (aggregateChunkStats, error) {
	whereBase := "is_regular_session = 1 AND market_date >= {from:Date} AND market_date <= {to:Date}"
	whereTarget := "toDate(timestamp) >= {from:Date} AND toDate(timestamp) <= {to:Date}"
	args := []any{
		clickhouse.Named("from", req.From.Format("2006-01-02")),
		clickhouse.Named("to", req.To.Format("2006-01-02")),
	}
	if len(req.FilterKeys) > 0 {
		whereBase += " AND " + req.KeyColumn + " IN ({keys:Array(String)})"
		whereTarget += " AND " + req.KeyColumn + " IN ({keys:Array(String)})"
		args = append(args, clickhouse.Named("keys", req.FilterKeys))
	}
	query := fmt.Sprintf(`WITH
base AS (
	SELECT market_date AS d, %s AS k
	FROM %s
	WHERE %s
	GROUP BY d, k
),
target AS (
	SELECT toDate(timestamp) AS d, %s AS k
	FROM %s
	WHERE %s
	GROUP BY d, k
),
missing AS (
	SELECT base.d AS d, base.k AS k
	FROM base
	LEFT JOIN target ON base.d = target.d AND base.k = target.k
	WHERE target.k = ''
)
SELECT
	(SELECT count() FROM base) AS base_keys,
	(SELECT count() FROM target) AS target_keys,
	count() AS missing_keys,
	toString(ifNull(min(d), toDate('1970-01-01'))) AS first_missing,
	toString(ifNull(max(d), toDate('1970-01-01'))) AS last_missing,
	groupArraySorted(%d)(concat(toString(d), ':', k)) AS samples,
	groupUniqArray(k) AS missing_group_keys
FROM missing`, req.KeyColumn, req.BaseTable, whereBase, req.KeyColumn, req.TargetTable, whereTarget, req.MaxSamples)
	query += req.Settings
	var baseKeys, targetKeys, missingKeys uint64
	var firstMissing, lastMissing string
	var samples []string
	var missingKeySet []string
	if err := c.conn.QueryRow(ctx, query, args...).Scan(&baseKeys, &targetKeys, &missingKeys, &firstMissing, &lastMissing, &samples, &missingKeySet); err != nil {
		return aggregateChunkStats{}, fmt.Errorf("check %s %s: %w", req.TargetTable, req.Interval, err)
	}
	return aggregateChunkStats{
		BaseKeys:      baseKeys,
		TargetKeys:    targetKeys,
		MissingKeys:   missingKeys,
		FirstMissing:  normalizeEmptyDate(firstMissing),
		LastMissing:   normalizeEmptyDate(lastMissing),
		Samples:       samples,
		MissingKeySet: missingKeySet,
	}, nil
}

func (c *Checker) repairUSOptionsAggregates(ctx context.Context, req Request, findings []Finding) []RepairAction {
	months, intervals := affectedMonthsFromFindings(findings)
	scope := scopeDescription(months, intervals)
	if !req.Repair {
		return []RepairAction{{Target: TargetUSOptionsAggregates, Action: "rebuild option kline aggregates", Status: "planned", Message: "pass --repair to rebuild " + scope}}
	}
	if req.DryRun {
		return []RepairAction{{Target: TargetUSOptionsAggregates, Action: "rebuild option kline aggregates", Status: "dry-run", Message: "would rebuild " + scope}}
	}
	opts := usmarket.AggregateRebuildOptions{
		Months:                        months,
		Intervals:                     intervals,
		MaxMemoryUsageBytes:           req.MaxMemoryUsageBytes,
		MaxBytesBeforeExternalGroupBy: req.MaxBytesBeforeExternalGroupBy,
		MaxThreads:                    req.MaxThreads,
	}
	req.progressf("integrity progress: repair us_options_bar_*_agg scope=%s", scope)
	if err := usmarket.RebuildOptionKlineAggregatesScoped(ctx, c.conn, opts); err != nil {
		return []RepairAction{{Target: TargetUSOptionsAggregates, Action: "rebuild option kline aggregates", Status: "failed", Message: err.Error()}}
	}
	return []RepairAction{{Target: TargetUSOptionsAggregates, Action: "rebuild option kline aggregates", Status: "completed", Message: "rebuilt " + scope}}
}

func (c *Checker) repairUSStocksAggregates(ctx context.Context, req Request, findings []Finding) []RepairAction {
	months, intervals := affectedMonthsFromFindings(findings)
	scope := scopeDescription(months, intervals)
	if !req.Repair {
		return []RepairAction{{Target: TargetUSStocksAggregates, Action: "rebuild stock kline aggregates", Status: "planned", Message: "pass --repair to rebuild " + scope}}
	}
	if req.DryRun {
		return []RepairAction{{Target: TargetUSStocksAggregates, Action: "rebuild stock kline aggregates", Status: "dry-run", Message: "would rebuild " + scope}}
	}
	opts := usmarket.AggregateRebuildOptions{
		Months:                        months,
		Intervals:                     intervals,
		MaxMemoryUsageBytes:           req.MaxMemoryUsageBytes,
		MaxBytesBeforeExternalGroupBy: req.MaxBytesBeforeExternalGroupBy,
		MaxThreads:                    req.MaxThreads,
	}
	req.progressf("integrity progress: repair us_stocks_bar_*_agg scope=%s", scope)
	if err := usmarket.RebuildStockKlineAggregatesScoped(ctx, c.conn, opts); err != nil {
		return []RepairAction{{Target: TargetUSStocksAggregates, Action: "rebuild stock kline aggregates", Status: "failed", Message: err.Error()}}
	}
	return []RepairAction{{Target: TargetUSStocksAggregates, Action: "rebuild stock kline aggregates", Status: "completed", Message: "rebuilt " + scope}}
}

func (c *Checker) repairOptionChainCache(ctx context.Context, req Request, findings []Finding) []RepairAction {
	months, intervals := affectedMonthsFromFindings(findings)
	scope := scopeDescription(months, intervals)
	if !req.Repair {
		return []RepairAction{{Target: TargetOptionChainCache, Action: "rebuild option chain caches", Status: "planned", Message: "pass --repair to rebuild " + scope}}
	}
	if req.DryRun {
		return []RepairAction{{Target: TargetOptionChainCache, Action: "rebuild option chain caches", Status: "dry-run", Message: "would rebuild " + scope}}
	}
	opts := usmarket.AggregateRebuildOptions{
		Months:                        months,
		Intervals:                     intervals,
		MaxMemoryUsageBytes:           req.MaxMemoryUsageBytes,
		MaxBytesBeforeExternalGroupBy: req.MaxBytesBeforeExternalGroupBy,
		MaxThreads:                    req.MaxThreads,
	}
	req.progressf("integrity progress: repair us_options_chain_*_agg scope=%s", scope)
	if err := usmarket.RebuildOptionChainCachesScoped(ctx, c.conn, opts); err != nil {
		return []RepairAction{{Target: TargetOptionChainCache, Action: "rebuild option chain caches", Status: "failed", Message: err.Error()}}
	}
	return []RepairAction{{Target: TargetOptionChainCache, Action: "rebuild option chain caches", Status: "completed", Message: "rebuilt " + scope}}
}

func (c *Checker) repairFundamentals(ctx context.Context, req Request, findings []Finding) []RepairAction {
	scope := fundamentalsScopeDescription(req)
	if !req.Repair {
		return []RepairAction{{Target: TargetFundamentals, Action: "backfill PE/PB fundamentals", Status: "planned", Message: "pass --repair to backfill " + scope}}
	}
	if req.DryRun {
		return []RepairAction{{Target: TargetFundamentals, Action: "backfill PE/PB fundamentals", Status: "dry-run", Message: "would backfill " + scope}}
	}
	if strings.TrimSpace(req.ClickHouseDSN) == "" {
		return []RepairAction{{Target: TargetFundamentals, Action: "backfill PE/PB fundamentals", Status: "failed", Message: "ClickHouse DSN is required for fundamentals repair"}}
	}
	if strings.TrimSpace(req.FMPAPIKey) == "" {
		return []RepairAction{{Target: TargetFundamentals, Action: "backfill PE/PB fundamentals", Status: "failed", Message: "FMP API key is required for fundamentals repair"}}
	}
	provider := usmarket.NewFMPPEBackfillProvider(req.FMPAPIKey, req.FMPQuarterLimit)
	req.progressf("integrity progress: repair fundamentals scope=%s", scope)
	result, err := usmarket.BackfillUSStockPE(ctx, usmarket.USFundamentalsBackfillConfig{
		Conn:               c.conn,
		DSN:                req.ClickHouseDSN,
		Provider:           provider,
		StartDate:          req.From,
		EndDate:            req.To,
		Symbols:            req.Symbols,
		Workers:            req.FundamentalWorkers,
		BatchSize:          req.FundamentalBatchSize,
		PageSize:           req.FundamentalPageSize,
		QPS:                req.FundamentalQPS,
		LimitSymbols:       req.FundamentalLimitSymbols,
		DryRun:             false,
		DistributedLimiter: req.FundamentalDistributedLimiter,
	})
	if err != nil {
		message := err.Error()
		if len(result.ThrottledSymbols) > 0 {
			message = fmt.Sprintf("%s (throttled_symbols=%d)", message, len(result.ThrottledSymbols))
		}
		return []RepairAction{{Target: TargetFundamentals, Action: "backfill PE/PB fundamentals", Status: "failed", Message: message}}
	}
	return []RepairAction{{Target: TargetFundamentals, Action: "backfill PE/PB fundamentals", Status: "completed", Message: fmt.Sprintf("backfilled %s inserted_rows=%d processed_symbols=%d failed_symbols=%d", scope, result.InsertedRows, result.ProcessedSymbols, result.FailedSymbols)}}
}

func (c *Checker) repairFeatures(ctx context.Context, req Request, findings []Finding) []RepairAction {
	scope := featureScopeDescription(req)
	if !req.Repair {
		return []RepairAction{{Target: TargetFeatures, Action: "backfill feature snapshots", Status: "planned", Message: "pass --repair to backfill " + scope}}
	}
	if req.DryRun {
		return []RepairAction{{Target: TargetFeatures, Action: "backfill feature snapshots", Status: "dry-run", Message: "would backfill " + scope}}
	}
	repo := chrepo.NewRepo(c.conn)
	featureSvc := service.NewFeatureService(repo)
	backfillReq := service.FeatureBackfillOptions{
		Markets:         []string{"us-options"},
		Underlyings:     req.Underlyings,
		PriorityOrder:   usmarket.PriorityOrderUSDefault,
		From:            req.From,
		To:              req.To.AddDate(0, 0, 1),
		LookbackDays:    req.LookbackDays,
		MinDaysToExpiry: req.MinDaysToExpiry,
		MaxDaysToExpiry: req.MaxDaysToExpiry,
		Workers:         1,
		Replace:         true,
		ContinueOnError: true,
		Progress: func(progress service.FeatureBackfillProgress) {
			if progress.Scope == "" && progress.Stage == "" && progress.Phase == "" {
				return
			}
			parts := []string{"integrity progress: repair features"}
			if progress.Market != "" {
				parts = append(parts, "market="+progress.Market)
			}
			if progress.Underlying != "" {
				parts = append(parts, "underlying="+progress.Underlying)
			}
			if progress.Scope != "" {
				parts = append(parts, "scope="+progress.Scope)
			}
			if progress.Stage != "" {
				parts = append(parts, "stage="+progress.Stage)
			}
			if progress.Phase != "" {
				parts = append(parts, "phase="+progress.Phase)
			}
			if progress.Outcome != "" {
				parts = append(parts, "outcome="+progress.Outcome)
			}
			if progress.RowsWritten > 0 {
				parts = append(parts, fmt.Sprintf("rows_written=%d", progress.RowsWritten))
			}
			req.progressf(strings.Join(parts, " "))
		},
	}
	req.progressf("integrity progress: repair features scope=%s", scope)
	stats, err := featureSvc.BackfillFeatureSnapshots(ctx, backfillReq)
	if err != nil {
		message := err.Error()
		if len(stats.Failures) > 0 {
			message = fmt.Sprintf("%s (failures=%d)", message, len(stats.Failures))
		}
		return []RepairAction{{Target: TargetFeatures, Action: "backfill feature snapshots", Status: "failed", Message: message}}
	}
	return []RepairAction{{Target: TargetFeatures, Action: "backfill feature snapshots", Status: "completed", Message: fmt.Sprintf("backfilled %s rows=%d scopes_replaced=%d", scope, stats.RowsWritten, stats.ScopesReplaced)}}
}

func featureRepairNeeded(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Target == TargetFeatures && finding.Severity != SeverityInfo {
			return true
		}
	}
	return false
}

func fundamentalsRepairNeeded(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Target == TargetFundamentals && finding.Severity != SeverityInfo {
			return true
		}
	}
	return false
}

func fundamentalsScopeDescription(req Request) string {
	parts := []string{fmt.Sprintf("window=%s..%s", req.From.Format("2006-01-02"), req.To.Format("2006-01-02"))}
	if len(req.Symbols) > 0 {
		parts = append(parts, "symbols="+strings.Join(req.Symbols, ","))
	} else {
		parts = append(parts, "symbols=all")
	}
	parts = append(parts, "provider=fmp")
	return strings.Join(parts, " ")
}

func featureScopeDescription(req Request) string {
	parts := []string{fmt.Sprintf("window=%s..%s", req.From.Format("2006-01-02"), req.To.Format("2006-01-02"))}
	if len(req.Underlyings) > 0 {
		parts = append(parts, "underlyings="+strings.Join(req.Underlyings, ","))
	} else {
		parts = append(parts, "underlyings=all")
	}
	parts = append(parts, fmt.Sprintf("lookback_days=%d", req.LookbackDays))
	parts = append(parts, fmt.Sprintf("dte=%d..%d", req.MinDaysToExpiry, req.MaxDaysToExpiry))
	return strings.Join(parts, " ")
}

func scopeDescription(months, intervals []string) string {
	switch {
	case len(months) == 0 && len(intervals) == 0:
		return "all aggregate tables (full history)"
	case len(months) == 0:
		return fmt.Sprintf("intervals=%s (full history)", strings.Join(intervals, ","))
	case len(intervals) == 0:
		return fmt.Sprintf("months=%s (all intervals)", strings.Join(months, ","))
	default:
		return fmt.Sprintf("intervals=%s months=%s", strings.Join(intervals, ","), strings.Join(months, ","))
	}
}

func (c *Checker) checkFundamentals(ctx context.Context, req Request) ([]Finding, error) {
	req.progressf("integrity progress: target=%s started", TargetFundamentals)
	whereStocks := "market_date >= {from:Date} AND market_date <= {to:Date}"
	whereFundamentals := "market = 'us-stocks' AND factor_code IN ('pe','pb')"
	args := []any{clickhouse.Named("from", req.From.Format("2006-01-02")), clickhouse.Named("to", req.To.Format("2006-01-02"))}
	if len(req.Symbols) > 0 {
		whereStocks += " AND symbol IN ({symbols:Array(String)})"
		whereFundamentals += " AND symbol IN ({symbols:Array(String)})"
		args = append(args, clickhouse.Named("symbols", req.Symbols))
	}
	query := fmt.Sprintf(`WITH
stock_symbols AS (
	SELECT symbol FROM us_stocks_bar_1m WHERE %s GROUP BY symbol
),
factors AS (
	SELECT symbol, factor_code, max(known_at) AS latest_known
	FROM fundamental_observation
	WHERE %s
	GROUP BY symbol, factor_code
),
missing AS (
	SELECT stock_symbols.symbol AS symbol, factor
	FROM stock_symbols
	ARRAY JOIN ['pe','pb'] AS factor
	LEFT JOIN factors ON stock_symbols.symbol = factors.symbol AND factor = factors.factor_code
	WHERE factors.symbol = ''
),
stale AS (
	SELECT symbol, factor_code, latest_known
	FROM factors
	WHERE latest_known < parseDateTimeBestEffort({stale_before:String})
)
SELECT
	(SELECT count() FROM stock_symbols) AS base_symbols,
	(SELECT count() FROM missing) AS missing_factor_symbols,
	(SELECT uniqExact(symbol) FROM missing) AS affected_symbols,
	(SELECT count() FROM stale) AS stale_factor_symbols`, whereStocks, whereFundamentals)
	query += req.clickhouseSettingsClause()
	args = append(args, clickhouse.Named("stale_before", time.Now().UTC().Add(-req.FundamentalStale).Format("2006-01-02 15:04:05")))
	var baseSymbols, missingFactors, affectedSymbols, staleFactors uint64
	if err := c.conn.QueryRow(ctx, query, args...).Scan(&baseSymbols, &missingFactors, &affectedSymbols, &staleFactors); err != nil {
		return nil, fmt.Errorf("check fundamentals: %w", err)
	}
	findings := []Finding{{
		Target:       TargetFundamentals,
		Check:        "pe-pb-coverage",
		Severity:     severityForCount(missingFactors),
		Message:      fmt.Sprintf("PE/PB coverage missing %d factor-symbol pairs across %d stock symbols", missingFactors, baseSymbols),
		Table:        "fundamental_observation",
		BaseKeys:     baseSymbols * 2,
		MissingKeys:  missingFactors,
		AffectedKeys: affectedSymbols,
	}}
	if baseSymbols > 0 && missingFactors > 0 {
		findings[0].MissingRatio = float64(missingFactors) / float64(baseSymbols*2)
		findings[0].Samples = c.bestEffortSamples(ctx, fmt.Sprintf(`WITH
stock_symbols AS (SELECT symbol FROM us_stocks_bar_1m WHERE %s GROUP BY symbol),
factors AS (SELECT symbol, factor_code FROM fundamental_observation WHERE %s GROUP BY symbol, factor_code)
SELECT concat(stock_symbols.symbol, ':', factor)
FROM stock_symbols ARRAY JOIN ['pe','pb'] AS factor
LEFT JOIN factors ON stock_symbols.symbol = factors.symbol AND factor = factors.factor_code
WHERE factors.symbol = ''
ORDER BY stock_symbols.symbol, factor
LIMIT %d`, whereStocks, whereFundamentals, req.MaxSamples)+req.clickhouseSettingsClause(), args)
		findings[0].Offenders = c.bestEffortSamples(ctx, fmt.Sprintf(`WITH
stock_symbols AS (SELECT symbol FROM us_stocks_bar_1m WHERE %s GROUP BY symbol),
factors AS (SELECT symbol, factor_code FROM fundamental_observation WHERE %s GROUP BY symbol, factor_code)
SELECT concat(stock_symbols.symbol, ':missing=', toString(count()))
FROM stock_symbols ARRAY JOIN ['pe','pb'] AS factor
LEFT JOIN factors ON stock_symbols.symbol = factors.symbol AND factor = factors.factor_code
WHERE factors.symbol = ''
GROUP BY stock_symbols.symbol
ORDER BY count() DESC, stock_symbols.symbol ASC
LIMIT %d`, whereStocks, whereFundamentals, req.MaxSamples)+req.clickhouseSettingsClause(), args)
	}
	findings = append(findings, Finding{
		Target:      TargetFundamentals,
		Check:       "pe-pb-freshness",
		Severity:    severityForCount(staleFactors),
		Message:     fmt.Sprintf("PE/PB has %d stale factor-symbol pairs older than %s", staleFactors, req.FundamentalStale),
		Table:       "fundamental_observation",
		MissingKeys: staleFactors,
	})
	if staleFactors > 0 {
		findings[1].Offenders = c.bestEffortSamples(ctx, fmt.Sprintf(`WITH latest AS (
SELECT symbol, factor_code, max(known_at) AS latest_known
FROM fundamental_observation
WHERE %s
GROUP BY symbol, factor_code
)
SELECT concat(symbol, ':stale=', toString(count()), ':latest_known=', toString(max(latest_known)))
FROM latest
WHERE latest_known < parseDateTimeBestEffort({stale_before:String})
GROUP BY symbol
ORDER BY count() DESC, symbol ASC
LIMIT %d`, whereFundamentals, req.MaxSamples)+req.clickhouseSettingsClause(), args)
	}
	req.progressf("integrity progress: target=%s finished findings=%d", TargetFundamentals, len(findings))
	return findings, nil
}

func (c *Checker) checkFeatures(ctx context.Context, req Request) ([]Finding, error) {
	req.progressf("integrity progress: target=%s started", TargetFeatures)
	volatility, err := c.checkVolatilityFeatures(ctx, req)
	if err != nil {
		return nil, err
	}
	panel, err := c.checkDailyPanelFeatures(ctx, req)
	if err != nil {
		return nil, err
	}
	findings := make([]Finding, 0, len(volatility)+len(panel))
	findings = append(findings, volatility...)
	findings = append(findings, panel...)
	req.progressf("integrity progress: target=%s finished findings=%d", TargetFeatures, len(findings))
	return findings, nil
}

func (c *Checker) checkVolatilityFeatures(ctx context.Context, req Request) ([]Finding, error) {
	where := "market = 'us-options' AND lookback_days = {lookback_days:UInt16} AND as_of_date >= {from:Date} AND as_of_date <= {to:Date}"
	args := []any{
		clickhouse.Named("lookback_days", uint16(req.LookbackDays)),
		clickhouse.Named("from", req.From.Format("2006-01-02")),
		clickhouse.Named("to", req.To.Format("2006-01-02")),
	}
	if len(req.Underlyings) > 0 {
		where += " AND underlying IN ({underlyings:Array(String)})"
		args = append(args, clickhouse.Named("underlyings", req.Underlyings))
	}
	query := fmt.Sprintf(`SELECT
	count() AS rows,
	countIf(hv10 IS NULL OR hv20 IS NULL OR hv30 IS NULL) AS missing_hv,
	countIf(current_iv IS NULL) AS missing_iv,
	countIf(iv_percentile IS NOT NULL AND (iv_percentile < 0 OR iv_percentile > 100)) AS bad_percentile,
	countIf(iv_rank IS NOT NULL AND (iv_rank < 0 OR iv_rank > 100)) AS bad_rank,
	toString(ifNull(max(updated_at), toDateTime(0, 'UTC'))) AS latest_update
FROM feature_volatility_snapshot_daily
WHERE %s`, where) + req.clickhouseSettingsClause()
	var rows, missingHV, missingIV, badPercentile, badRank uint64
	var latestUpdate string
	if err := c.conn.QueryRow(ctx, query, args...).Scan(&rows, &missingHV, &missingIV, &badPercentile, &badRank, &latestUpdate); err != nil {
		return nil, fmt.Errorf("check volatility features: %w", err)
	}
	findings := []Finding{
		featureFinding("volatility-hv", "feature_volatility_snapshot_daily", missingHV, rows, "volatility rows missing one of hv10/hv20/hv30"),
		featureFinding("volatility-iv", "feature_volatility_snapshot_daily", missingIV, rows, "volatility rows missing current_iv"),
		featureFinding("volatility-iv-percentile-bounds", "feature_volatility_snapshot_daily", badPercentile, rows, "volatility rows have iv_percentile outside 0..100"),
		featureFinding("volatility-iv-rank-bounds", "feature_volatility_snapshot_daily", badRank, rows, "volatility rows have iv_rank outside 0..100"),
	}
	if missingHV > 0 {
		findings[0].Offenders = c.bestEffortSamples(ctx, fmt.Sprintf(`SELECT concat(underlying, ':missing_hv=', toString(count()), ':lt30_price_obs=', toString(countIf(price_observations < 30)))
FROM feature_volatility_snapshot_daily
WHERE %s AND (hv10 IS NULL OR hv20 IS NULL OR hv30 IS NULL)
GROUP BY underlying
ORDER BY count() DESC, underlying ASC
LIMIT %d`, where, req.MaxSamples)+req.clickhouseSettingsClause(), args)
	}
	if missingIV > 0 {
		findings[1].Offenders = c.bestEffortSamples(ctx, fmt.Sprintf(`SELECT concat(underlying, ':missing_iv=', toString(count()), ':zero_iv_obs=', toString(countIf(iv_observations = 0)))
FROM feature_volatility_snapshot_daily
WHERE %s AND current_iv IS NULL
GROUP BY underlying
ORDER BY count() DESC, underlying ASC
LIMIT %d`, where, req.MaxSamples)+req.clickhouseSettingsClause(), args)
	}
	if isStaleDateTime(latestUpdate, req.FeatureStale) {
		findings = append(findings, Finding{Target: TargetFeatures, Check: "volatility-freshness", Severity: SeverityWarning, Table: "feature_volatility_snapshot_daily", Message: fmt.Sprintf("latest volatility feature update %s is older than %s", latestUpdate, req.FeatureStale), MissingKeys: 1})
	} else {
		findings = append(findings, Finding{Target: TargetFeatures, Check: "volatility-freshness", Severity: SeverityInfo, Table: "feature_volatility_snapshot_daily", Message: "volatility feature update freshness ok"})
	}
	return findings, nil
}

func (c *Checker) checkDailyPanelFeatures(ctx context.Context, req Request) ([]Finding, error) {
	where := "market = 'us-options' AND lookback_days = {lookback_days:UInt16} AND min_days_to_expiry = {min_dte:Int32} AND max_days_to_expiry = {max_dte:Int32} AND as_of_date >= {from:Date} AND as_of_date <= {to:Date}"
	args := []any{
		clickhouse.Named("lookback_days", uint16(req.LookbackDays)),
		clickhouse.Named("min_dte", int32(req.MinDaysToExpiry)),
		clickhouse.Named("max_dte", int32(req.MaxDaysToExpiry)),
		clickhouse.Named("from", req.From.Format("2006-01-02")),
		clickhouse.Named("to", req.To.Format("2006-01-02")),
	}
	if len(req.Underlyings) > 0 {
		where += " AND underlying IN ({underlyings:Array(String)})"
		args = append(args, clickhouse.Named("underlyings", req.Underlyings))
	}
	query := fmt.Sprintf(`SELECT
	count() AS rows,
	countIf(hv10 IS NULL OR hv20 IS NULL OR hv30 IS NULL) AS missing_hv,
	countIf(current_iv IS NULL) AS missing_iv,
	countIf(liquidity_contract_count = 0) AS missing_liquidity,
	toString(ifNull(max(updated_at), toDateTime(0, 'UTC'))) AS latest_update
FROM feature_daily_panel_daily
WHERE %s`, where) + req.clickhouseSettingsClause()
	var rows, missingHV, missingIV, missingLiquidity uint64
	var latestUpdate string
	if err := c.conn.QueryRow(ctx, query, args...).Scan(&rows, &missingHV, &missingIV, &missingLiquidity, &latestUpdate); err != nil {
		return nil, fmt.Errorf("check daily panel features: %w", err)
	}
	findings := []Finding{
		featureFinding("daily-panel-hv", "feature_daily_panel_daily", missingHV, rows, "daily panel rows missing one of hv10/hv20/hv30"),
		featureFinding("daily-panel-iv", "feature_daily_panel_daily", missingIV, rows, "daily panel rows missing current_iv"),
		featureFinding("daily-panel-liquidity", "feature_daily_panel_daily", missingLiquidity, rows, "daily panel rows have zero liquidity_contract_count"),
	}
	if missingHV > 0 {
		findings[0].Offenders = c.bestEffortSamples(ctx, fmt.Sprintf(`SELECT concat(underlying, ':missing_hv=', toString(count()), ':lt30_price_obs=', toString(countIf(price_observations < 30)))
FROM feature_daily_panel_daily
WHERE %s AND (hv10 IS NULL OR hv20 IS NULL OR hv30 IS NULL)
GROUP BY underlying
ORDER BY count() DESC, underlying ASC
LIMIT %d`, where, req.MaxSamples)+req.clickhouseSettingsClause(), args)
	}
	if missingIV > 0 {
		findings[1].Offenders = c.bestEffortSamples(ctx, fmt.Sprintf(`SELECT concat(underlying, ':missing_iv=', toString(count()), ':zero_iv_obs=', toString(countIf(iv_observations = 0)))
FROM feature_daily_panel_daily
WHERE %s AND current_iv IS NULL
GROUP BY underlying
ORDER BY count() DESC, underlying ASC
LIMIT %d`, where, req.MaxSamples)+req.clickhouseSettingsClause(), args)
	}
	if missingLiquidity > 0 {
		findings[2].Offenders = c.bestEffortSamples(ctx, fmt.Sprintf(`SELECT concat(underlying, ':missing_liquidity=', toString(count()), ':zero_tradable=', toString(countIf(liquidity_tradable_contract_count = 0)), ':zero_surface=', toString(countIf(surface_contract_count <= 0)))
FROM feature_daily_panel_daily
WHERE %s AND liquidity_contract_count = 0
GROUP BY underlying
ORDER BY count() DESC, underlying ASC
LIMIT %d`, where, req.MaxSamples)+req.clickhouseSettingsClause(), args)
	}
	if isStaleDateTime(latestUpdate, req.FeatureStale) {
		findings = append(findings, Finding{Target: TargetFeatures, Check: "daily-panel-freshness", Severity: SeverityWarning, Table: "feature_daily_panel_daily", Message: fmt.Sprintf("latest daily panel update %s is older than %s", latestUpdate, req.FeatureStale), MissingKeys: 1})
	} else {
		findings = append(findings, Finding{Target: TargetFeatures, Check: "daily-panel-freshness", Severity: SeverityInfo, Table: "feature_daily_panel_daily", Message: "daily panel update freshness ok"})
	}
	return findings, nil
}

type inclusiveDateWindow struct {
	From time.Time
	To   time.Time
}

func splitMonthlyWindowsInclusive(from, to time.Time) []inclusiveDateWindow {
	from = dateOnly(from)
	to = dateOnly(to)
	if to.Before(from) {
		return nil
	}
	approxMonths := (to.Year()-from.Year())*12 + int(to.Month()-from.Month()) + 1
	if approxMonths < 1 {
		approxMonths = 1
	}
	windows := make([]inclusiveDateWindow, 0, approxMonths)
	cursor := from
	for !cursor.After(to) {
		nextMonth := time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		windowTo := nextMonth.AddDate(0, 0, -1)
		if windowTo.After(to) {
			windowTo = to
		}
		windows = append(windows, inclusiveDateWindow{From: cursor, To: windowTo})
		cursor = windowTo.AddDate(0, 0, 1)
	}
	return windows
}

func mergeMissingWindow(finding *Finding, first, last string) {
	if first != "" && (finding.FirstMissingDate == "" || first < finding.FirstMissingDate) {
		finding.FirstMissingDate = first
	}
	if last != "" && (finding.LastMissingDate == "" || last > finding.LastMissingDate) {
		finding.LastMissingDate = last
	}
}

func appendSamples(dst *[]string, src []string, limit int) {
	if limit <= 0 || len(*dst) >= limit {
		return
	}
	for _, sample := range src {
		if len(*dst) >= limit {
			return
		}
		*dst = append(*dst, sample)
	}
}

func featureFinding(check, table string, missing, total uint64, label string) Finding {
	severity := severityForCount(missing)
	message := fmt.Sprintf("%s: %d of %d rows", label, missing, total)
	return Finding{Target: TargetFeatures, Check: check, Severity: severity, Message: message, Table: table, BaseKeys: total, MissingKeys: missing, MissingRatio: ratio(missing, total)}
}

func (c *Checker) bestEffortSamples(ctx context.Context, query string, args []any) []string {
	rows, err := c.conn.Query(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var samples []string
	for rows.Next() {
		var sample string
		if err := rows.Scan(&sample); err != nil {
			return samples
		}
		samples = append(samples, sample)
	}
	return samples
}

func severityForCount(count uint64) Severity {
	if count == 0 {
		return SeverityInfo
	}
	return SeverityCritical
}

func ratio(part, total uint64) float64 {
	if total == 0 || part == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func normalizeEmptyDate(value string) string {
	if value == "" || value == "1970-01-01" {
		return ""
	}
	return value
}

func isStaleDateTime(value string, staleAfter time.Duration) bool {
	if value == "" || strings.HasPrefix(value, "1970-01-01") {
		return true
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		return false
	}
	return time.Since(parsed) > staleAfter
}

func dateOnly(value time.Time) time.Time {
	value = value.UTC()
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
