package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/requestpriority"
	"github.com/Cyvadra/toktik/internal/universerepo"
)

const (
	defaultUniverseMarket      = "us-stocks"
	defaultUniverseLimit       = 5000
	maxUniverseIntervalMembers = 500000
)

type universeDefinitionRepo interface {
	UpsertDefinition(ctx context.Context, definition universerepo.Definition) error
	UpsertRun(ctx context.Context, run universerepo.Run) error
}

type UniverseService struct {
	repo          *chrepo.Repo
	definitions   universeDefinitionRepo
	etfClassifier usStockCompanyProfileProvider
	now           func() time.Time
	rebuildStart  time.Time

	rebuildLocksMu sync.Mutex
	rebuildLocks   map[universeLockKey]*sync.Mutex

	rebuildJobsMu sync.Mutex
	rebuildJobs   map[string]universeRebuildJob
}

// universeLockKey scopes rebuild serialization to a single (market, code)
// universe so unrelated universes can still rebuild concurrently.
type universeLockKey struct {
	market string
	code   string
}

type universeRebuildJob struct {
	market      string
	code        string
	sourceType  dto.UniverseSourceType
	requestHash string
	startedAt   time.Time
}

type UniverseIntervalProvider struct {
	members map[string][]dto.UniverseMember
}

func NewUniverseService(repo *chrepo.Repo, definitions universeDefinitionRepo) *UniverseService {
	return &UniverseService{repo: repo, definitions: definitions, now: time.Now, rebuildLocks: make(map[universeLockKey]*sync.Mutex), rebuildJobs: make(map[string]universeRebuildJob)}
}

// lockUniverse serializes ReplaceMembers calls for a given (market, code) so
// concurrent rebuild requests cannot interleave delete/insert operations on
// the same universe. Returns an unlock function; safe to call even if the
// service was constructed without NewUniverseService (map is lazily created).
func (s *UniverseService) lockUniverse(market, code string) func() {
	key := universeLockKey{market: normalizeUniverseMarket(market), code: normalizeUniverseCode(code)}
	s.rebuildLocksMu.Lock()
	if s.rebuildLocks == nil {
		s.rebuildLocks = make(map[universeLockKey]*sync.Mutex)
	}
	mu, ok := s.rebuildLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		s.rebuildLocks[key] = mu
	}
	s.rebuildLocksMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (s *UniverseService) WithETFClassifier(provider usStockCompanyProfileProvider) *UniverseService {
	if s == nil {
		return nil
	}
	s.etfClassifier = provider
	return s
}

// WithRebuildStart configures the inclusive start date used for force-refresh
// universe rebuilds.
func (s *UniverseService) WithRebuildStart(start time.Time) *UniverseService {
	if s == nil {
		return s
	}
	if start.IsZero() {
		s.rebuildStart = time.Time{}
		return s
	}
	s.rebuildStart = normalizeUniverseDate(start, start)
	return s
}

func (s *UniverseService) Members(ctx context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error) {
	asOf := normalizeUniverseDate(req.AsOf, s.now())
	req.From = asOf
	req.To = asOf.AddDate(0, 0, 1)
	resp, err := s.MemberIntervals(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.AsOf = &asOf
	resp.From = time.Time{}
	resp.To = time.Time{}
	return resp, nil
}

func (s *UniverseService) MemberIntervals(ctx context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error) {
	market := normalizeUniverseMarket(req.Market)
	code := normalizeUniverseCode(req.Code)
	if code == "" {
		return nil, dto.NewValidationError("universe code must be non-empty")
	}
	from := normalizeUniverseDate(req.From, s.now())
	to := normalizeUniverseDate(req.To, from.AddDate(0, 0, 1))
	if !to.After(from) {
		return nil, dto.NewValidationError("universe to must be after from")
	}
	limit := clamp(req.Limit, defaultUniverseLimit, maxUniverseIntervalMembers)

	query := fmt.Sprintf(`
SELECT universe_code_value, market_value, symbol, valid_from_value, valid_to_value, score_value, rank_value, source_run_id_value, metadata_value, source_value, ingested_at_value
FROM (
	SELECT
		argMax(universe_code, version) AS universe_code_value,
		argMax(market, version) AS market_value,
		symbol,
		argMax(valid_from, version) AS valid_from_value,
		argMax(valid_to, version) AS valid_to_value,
		argMax(score, version) AS score_value,
		argMax(rank, version) AS rank_value,
		argMax(source_run_id, version) AS source_run_id_value,
		argMax(metadata, version) AS metadata_value,
		argMax(source, version) AS source_value,
		argMax(ingested_at, version) AS ingested_at_value
	FROM universe_membership
	WHERE market = %s
		AND universe_code = %s
		AND valid_from < toDate(%s)
		AND valid_to > toDate(%s)
	GROUP BY symbol, valid_from, valid_to
)
ORDER BY valid_from_value, ifNull(rank_value, 4294967295), symbol
LIMIT %s`,
		clickhouseStringLiteral(market),
		clickhouseStringLiteral(code),
		clickhouseStringLiteral(to.Format("2006-01-02")),
		clickhouseStringLiteral(from.Format("2006-01-02")),
		clickhouseUInt32Literal(limit),
	)
	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query universe members: %w", err)
	}
	defer rows.Close()

	members := make([]dto.UniverseMember, 0)
	for rows.Next() {
		var member dto.UniverseMember
		if err := rows.Scan(&member.UniverseCode, &member.Market, &member.Symbol, &member.ValidFrom, &member.ValidTo, &member.Score, &member.Rank, &member.SourceRunID, &member.Metadata, &member.Source, &member.IngestedAt); err != nil {
			return nil, fmt.Errorf("scan universe member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate universe members: %w", err)
	}
	return &dto.UniverseMembersResponse{Market: market, Code: code, From: from, To: to, Data: members}, nil
}

// StartRebuild accepts a rebuild trigger and executes the heavy rebuild in the
// background. An identical normalized request hash already in flight is ignored
// so retries cannot pile up behind a long-running rebuild.
func (s *UniverseService) StartRebuild(ctx context.Context, req dto.UniverseRebuildRequest) (*dto.UniverseRebuildAccepted, error) {
	market := normalizeUniverseMarket(req.Market)
	code := normalizeUniverseCode(req.Code)
	if code == "" {
		return nil, dto.NewValidationError("universe code must be non-empty")
	}
	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = dto.UniverseSourceTurnoverIntersectionUnion
	}
	switch sourceType {
	case dto.UniverseSourceTurnoverIntersectionUnion, dto.UniverseSourcePresetSymbols, dto.UniverseSourceProviderHoldings:
	default:
		return nil, dto.NewValidationError("unsupported universe source_type %q", sourceType)
	}
	req.Market = market
	req.Code = code
	req.SourceType = sourceType
	requestHash, err := universeRebuildRequestHash(req)
	if err != nil {
		return nil, err
	}

	now := s.now()
	s.rebuildJobsMu.Lock()
	if s.rebuildJobs == nil {
		s.rebuildJobs = make(map[string]universeRebuildJob)
	}
	if job, ok := s.rebuildJobs[requestHash]; ok {
		s.rebuildJobsMu.Unlock()
		slog.Info("ignore universe rebuild trigger: identical request already running", "market", market, "code", code, "source_type", sourceType, "request_hash", requestHash, "started_at", job.startedAt)
		return &dto.UniverseRebuildAccepted{Market: market, Code: code, SourceType: sourceType, RequestHash: requestHash, Accepted: false, Ignored: true, Status: "already_running", Message: "identical universe rebuild is already running", StartedAt: job.startedAt}, nil
	}
	s.rebuildJobs[requestHash] = universeRebuildJob{market: market, code: code, sourceType: sourceType, requestHash: requestHash, startedAt: now}
	s.rebuildJobsMu.Unlock()

	slog.Info("accepted universe rebuild trigger", "market", market, "code", code, "source_type", sourceType, "request_hash", requestHash)
	go s.runRebuildJob(req, requestHash, now)
	return &dto.UniverseRebuildAccepted{Market: market, Code: code, SourceType: sourceType, RequestHash: requestHash, Accepted: true, Ignored: false, Status: "queued", Message: "universe rebuild started", StartedAt: now}, nil
}

func (s *UniverseService) runRebuildJob(req dto.UniverseRebuildRequest, requestHash string, startedAt time.Time) {
	defer func() {
		s.rebuildJobsMu.Lock()
		delete(s.rebuildJobs, requestHash)
		s.rebuildJobsMu.Unlock()
	}()
	ctx := requestpriority.WithBackground(context.Background())
	slog.Info("start universe rebuild", "market", req.Market, "code", req.Code, "source_type", req.SourceType, "request_hash", requestHash, "started_at", startedAt)
	resp, err := s.Rebuild(ctx, req)
	if err != nil {
		slog.Error("universe rebuild failed", "market", req.Market, "code", req.Code, "source_type", req.SourceType, "request_hash", requestHash, "latency_ms", time.Since(startedAt).Milliseconds(), "error", err)
		return
	}
	slog.Info("universe rebuild completed", "market", req.Market, "code", req.Code, "source_type", req.SourceType, "request_hash", requestHash, "run_id", resp.RunID, "from", resp.From.Format("2006-01-02"), "to", resp.To.Format("2006-01-02"), "member_count", resp.MemberCount, "dry_run", resp.DryRun, "latency_ms", time.Since(startedAt).Milliseconds())
}

func (p *UniverseIntervalProvider) SymbolsAt(code string, ts time.Time) []string {
	if p == nil {
		return nil
	}
	code = normalizeUniverseCode(code)
	day := normalizeUniverseDate(ts, ts)
	members := p.members[code]
	if len(members) == 0 {
		return nil
	}
	out := make([]string, 0, len(members))
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		if day.Before(normalizeUniverseDate(member.ValidFrom, member.ValidFrom)) || !day.Before(normalizeUniverseDate(member.ValidTo, member.ValidTo)) {
			continue
		}
		symbol := normalizeSymbol(member.Symbol)
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out
}

// Rebuild derives its persistence interval from the shared latest SPY stock
// and option dates, then calculates and optionally persists named membership.
// ForceRefresh selects a full history rebuild; otherwise it resumes after the
// universe's latest valid_to date.
func (s *UniverseService) Rebuild(ctx context.Context, req dto.UniverseRebuildRequest) (*dto.UniverseRebuildResponse, error) {
	market := normalizeUniverseMarket(req.Market)
	code := normalizeUniverseCode(req.Code)
	if code == "" {
		return nil, dto.NewValidationError("universe code must be non-empty")
	}
	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = dto.UniverseSourceTurnoverIntersectionUnion
	}
	from, to, noWork, err := s.deriveUniverseRebuildRange(ctx, market, code, req.ForceRefresh)
	if err != nil {
		return nil, err
	}
	if noWork {
		return &dto.UniverseRebuildResponse{Market: market, Code: code, SourceType: sourceType, AsOf: from, From: from, To: to, DryRun: req.DryRun}, nil
	}
	switch sourceType {
	case dto.UniverseSourceTurnoverIntersectionUnion:
		return s.rebuildTurnoverIntersectionUnion(ctx, req, market, code, from, to)
	case dto.UniverseSourcePresetSymbols, dto.UniverseSourceProviderHoldings:
		return s.rebuildStaticMembers(ctx, req, market, code, sourceType, from, to)
	default:
		return nil, dto.NewValidationError("unsupported universe source_type %q", sourceType)
	}
}

// deriveUniverseRebuildRange derives [from, to) from database facts. The
// common reference date is the minimum of SPY stock and option daily data;
// the returned end is exclusive so it includes that latest trading date.
func (s *UniverseService) deriveUniverseRebuildRange(ctx context.Context, market, code string, forceRefresh bool) (from, to time.Time, noWork bool, err error) {
	if s.repo == nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("universe rebuild requires ClickHouse repository")
	}
	if s.rebuildStart.IsZero() {
		return time.Time{}, time.Time{}, false, fmt.Errorf("universe rebuild start is not configured")
	}
	dataWindow, hasData, err := s.universeReferenceDataWindow(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if !hasData {
		slog.Warn("skip universe rebuild: required daily stock or option reference data is unavailable", "market", market, "code", code)
		return time.Time{}, time.Time{}, true, nil
	}
	if err := validateUniverseRebuildStart(s.rebuildStart, dataWindow.earliest()); err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if forceRefresh {
		from, to, noWork = selectUniverseRebuildWindow(s.rebuildStart, dataWindow.latest(), time.Time{}, false, true)
		return from, to, noWork, nil
	}
	lastValidTo, hasMembers, err := s.latestUniverseValidTo(ctx, market, code)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	from, to, noWork = selectUniverseRebuildWindow(s.rebuildStart, dataWindow.latest(), lastValidTo, hasMembers, false)
	return from, to, noWork, nil
}

type universeReferenceDataWindow struct {
	stockEarliest   time.Time
	optionsEarliest time.Time
	stockLatest     time.Time
	optionsLatest   time.Time
}

func (w universeReferenceDataWindow) earliest() time.Time {
	return maximumUniverseReferenceDate(w.stockEarliest, w.optionsEarliest)
}

func (w universeReferenceDataWindow) latest() time.Time {
	return minimumUniverseReferenceDate(w.stockLatest, w.optionsLatest)
}

// selectUniverseRebuildWindow applies the server-owned rebuild policy using
// inclusive dates. Its returned end is exclusive for interval persistence.
func selectUniverseRebuildWindow(rebuildStart, latestReference, lastValidTo time.Time, hasMembers, forceRefresh bool) (from, to time.Time, noWork bool) {
	from = normalizeUniverseDate(rebuildStart, rebuildStart)
	to = normalizeUniverseDate(latestReference, latestReference).AddDate(0, 0, 1)
	if !forceRefresh && hasMembers && lastValidTo.After(from) {
		from = normalizeUniverseDate(lastValidTo, lastValidTo)
	}
	return from, to, !to.After(from)
}

func validateUniverseRebuildStart(rebuildStart, earliestReference time.Time) error {
	if normalizeUniverseDate(rebuildStart, rebuildStart).Before(earliestReference) {
		return dto.NewValidationError("universe rebuild_start_date %s is before earliest common daily market data %s", rebuildStart.Format("2006-01-02"), earliestReference.Format("2006-01-02"))
	}
	return nil
}

func (s *UniverseService) universeReferenceDataWindow(ctx context.Context) (universeReferenceDataWindow, bool, error) {
	rows, err := s.repo.Query(ctx, `
SELECT
	(SELECT minOrNull(timestamp) FROM us_stocks_bar_1d) AS stock_earliest,
	(SELECT minOrNull(timestamp) FROM us_options_bar_1d) AS options_earliest,
	(SELECT maxOrNull(timestamp) FROM us_stocks_bar_1d WHERE symbol = 'SPY') AS stock_latest,
	(SELECT maxOrNull(timestamp) FROM us_options_bar_1d WHERE underlying = 'SPY') AS options_latest`)
	if err != nil {
		return universeReferenceDataWindow{}, false, fmt.Errorf("query daily universe reference data: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return universeReferenceDataWindow{}, false, nil
	}
	var stockEarliest, optionsEarliest, stockLatest, optionsLatest *time.Time
	if err := rows.Scan(&stockEarliest, &optionsEarliest, &stockLatest, &optionsLatest); err != nil {
		return universeReferenceDataWindow{}, false, fmt.Errorf("scan daily universe reference data: %w", err)
	}
	if err := rows.Err(); err != nil {
		return universeReferenceDataWindow{}, false, fmt.Errorf("iterate daily universe reference data: %w", err)
	}
	if stockEarliest == nil || optionsEarliest == nil || stockLatest == nil || optionsLatest == nil {
		return universeReferenceDataWindow{}, false, nil
	}
	return universeReferenceDataWindow{
		stockEarliest:   normalizeUniverseDate(*stockEarliest, *stockEarliest),
		optionsEarliest: normalizeUniverseDate(*optionsEarliest, *optionsEarliest),
		stockLatest:     normalizeUniverseDate(*stockLatest, *stockLatest),
		optionsLatest:   normalizeUniverseDate(*optionsLatest, *optionsLatest),
	}, true, nil
}

func minimumUniverseReferenceDate(stockLatest, optionsLatest time.Time) time.Time {
	stockLatest = normalizeUniverseDate(stockLatest, stockLatest)
	optionsLatest = normalizeUniverseDate(optionsLatest, optionsLatest)
	if optionsLatest.Before(stockLatest) {
		return optionsLatest
	}
	return stockLatest
}

func maximumUniverseReferenceDate(stockEarliest, optionsEarliest time.Time) time.Time {
	stockEarliest = normalizeUniverseDate(stockEarliest, stockEarliest)
	optionsEarliest = normalizeUniverseDate(optionsEarliest, optionsEarliest)
	if optionsEarliest.After(stockEarliest) {
		return optionsEarliest
	}
	return stockEarliest
}

func (s *UniverseService) latestUniverseValidTo(ctx context.Context, market, code string) (time.Time, bool, error) {
	query := fmt.Sprintf(`SELECT maxOrNull(valid_to) FROM universe_membership FINAL WHERE market = %s AND universe_code = %s`, clickhouseStringLiteral(market), clickhouseStringLiteral(code))
	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest universe validity: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var validTo *time.Time
	if err := rows.Scan(&validTo); err != nil {
		return time.Time{}, false, fmt.Errorf("scan latest universe validity: %w", err)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, fmt.Errorf("iterate latest universe validity: %w", err)
	}
	if validTo == nil {
		return time.Time{}, false, nil
	}
	return normalizeUniverseDate(*validTo, *validTo), true, nil
}

func (s *UniverseService) rebuildStaticMembers(ctx context.Context, req dto.UniverseRebuildRequest, market, code string, sourceType dto.UniverseSourceType, from, to time.Time) (*dto.UniverseRebuildResponse, error) {
	members := normalizeStaticUniverseMembers(req, market, code, sourceType, from, to)
	if len(members) == 0 {
		return nil, dto.NewValidationError("%s universe requires symbols or members", sourceType)
	}
	runID := universeRunID(code, market, sourceType, from, to, members)
	for index := range members {
		members[index].SourceRunID = runID
	}
	if !req.DryRun {
		if err := s.persistUniverseRunStatus(ctx, code, market, sourceType, from, to, nil, len(members), true, runID, len(members), "pending", nil); err != nil {
			return nil, err
		}
		if err := s.ReplaceMembers(ctx, market, code, from, to, members); err != nil {
			_ = s.persistUniverseRunStatus(ctx, code, market, sourceType, from, to, nil, len(members), true, runID, len(members), "failed", err)
			return nil, err
		}
		if err := s.persistUniverseRunStatus(ctx, code, market, sourceType, from, to, nil, len(members), true, runID, len(members), "success", nil); err != nil {
			return nil, err
		}
	}
	return &dto.UniverseRebuildResponse{Market: market, Code: code, SourceType: sourceType, AsOf: from, From: from, To: to, RunID: runID, DryRun: req.DryRun, MemberCount: len(members), Data: members}, nil
}

func normalizeStaticUniverseMembers(req dto.UniverseRebuildRequest, market, code string, sourceType dto.UniverseSourceType, from, to time.Time) []dto.UniverseMember {
	seen := make(map[string]struct{}, len(req.Symbols)+len(req.Members))
	members := make([]dto.UniverseMember, 0, len(req.Symbols)+len(req.Members))
	appendMember := func(member dto.UniverseMember) {
		symbol := normalizeSymbol(member.Symbol)
		if symbol == "" {
			return
		}
		if _, ok := seen[symbol]; ok {
			return
		}
		seen[symbol] = struct{}{}
		member.UniverseCode = code
		member.Market = market
		member.Symbol = symbol
		member.ValidFrom = normalizeUniverseDate(member.ValidFrom, from)
		member.ValidTo = normalizeUniverseDate(member.ValidTo, to)
		if member.ValidFrom.Before(from) {
			member.ValidFrom = from
		}
		if member.ValidTo.After(to) {
			member.ValidTo = to
		}
		if !member.ValidTo.After(member.ValidFrom) {
			return
		}
		member.Source = string(sourceType)
		members = append(members, member)
	}
	for _, symbol := range req.Symbols {
		appendMember(dto.UniverseMember{Symbol: symbol})
	}
	for _, member := range req.Members {
		appendMember(member)
	}
	sort.SliceStable(members, func(i, j int) bool { return members[i].Symbol < members[j].Symbol })
	for index := range members {
		rank := uint32(index + 1)
		if members[index].Rank == nil {
			members[index].Rank = &rank
		}
	}
	return members
}

func (s *UniverseService) rebuildTurnoverIntersectionUnion(ctx context.Context, req dto.UniverseRebuildRequest, market, code string, from, to time.Time) (*dto.UniverseRebuildResponse, error) {
	lookbackDays := normalizeUniverseLookbacks(req.LookbackDays)
	limit := req.Limit
	if limit <= 0 {
		limit = observedUSStockPoolTopLimit
	}
	if limit > turnoverIntersectionPoolCandidateKeep {
		limit = turnoverIntersectionPoolCandidateKeep
	}
	nonETFOnly := true
	if req.NonETFOnly != nil {
		nonETFOnly = *req.NonETFOnly
	}

	slog.Info("universe rebuild progress: ensuring turnover intersection source", "market", market, "code", code, "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"), "lookback_days", lookbackDays, "limit", limit, "non_etf_only", nonETFOnly, "force_rebuild_source", req.ForceRebuildSource)
	if err := s.ensureTurnoverIntersectionPool(ctx, market, lookbackDays, nonETFOnly, from, to, req.ForceRebuildSource); err != nil {
		return nil, err
	}

	slog.Info("universe rebuild progress: querying turnover intersection source", "market", market, "code", code, "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"), "lookback_days", lookbackDays, "limit", limit, "non_etf_only", nonETFOnly)
	members, err := s.queryCompressedTurnoverIntersectionMembers(ctx, market, code, lookbackDays, limit, nonETFOnly, from, to)
	if err != nil {
		return nil, err
	}
	slog.Info("universe rebuild progress: compressed turnover intersection members", "market", market, "code", code, "member_count", len(members))
	runID := universeRunID(code, market, dto.UniverseSourceTurnoverIntersectionUnion, from, to, members)
	for index := range members {
		members[index].SourceRunID = runID
	}
	if !req.DryRun {
		slog.Info("universe rebuild progress: persisting members", "market", market, "code", code, "run_id", runID, "member_count", len(members))
		if err := s.persistUniverseRunStatus(ctx, code, market, dto.UniverseSourceTurnoverIntersectionUnion, from, to, lookbackDays, limit, nonETFOnly, runID, len(members), "pending", nil); err != nil {
			return nil, err
		}
		if err := s.ReplaceMembers(ctx, market, code, from, to, members); err != nil {
			_ = s.persistUniverseRunStatus(ctx, code, market, dto.UniverseSourceTurnoverIntersectionUnion, from, to, lookbackDays, limit, nonETFOnly, runID, len(members), "failed", err)
			return nil, err
		}
		if err := s.persistUniverseRunStatus(ctx, code, market, dto.UniverseSourceTurnoverIntersectionUnion, from, to, lookbackDays, limit, nonETFOnly, runID, len(members), "success", nil); err != nil {
			return nil, err
		}
		slog.Info("universe rebuild progress: persisted members", "market", market, "code", code, "run_id", runID, "member_count", len(members))
	}
	return &dto.UniverseRebuildResponse{Market: market, Code: code, SourceType: dto.UniverseSourceTurnoverIntersectionUnion, AsOf: from, From: from, To: to, RunID: runID, DryRun: req.DryRun, MemberCount: len(members), LookbackDays: lookbackDays, Data: members}, nil
}

func (s *UniverseService) queryCompressedTurnoverIntersectionMembers(ctx context.Context, market, code string, lookbackDays []int, limit int, nonETFOnly bool, from, to time.Time) ([]dto.UniverseMember, error) {
	query := fmt.Sprintf(`
SELECT
	as_of_date,
	underlying,
	score,
	toUInt32(row_number() OVER (PARTITION BY as_of_date ORDER BY score DESC, underlying ASC)) AS member_rank
FROM (
	SELECT
		as_of_date,
		underlying,
		max(combined_turnover_usd) AS score
	FROM %s
	WHERE market = %s
		AND non_etf_only = %s
		AND lookback_days IN (%s)
		AND as_of_date >= toDate(%s)
		AND as_of_date < toDate(%s)
		AND rank <= %s
	GROUP BY as_of_date, underlying
)
ORDER BY as_of_date ASC, member_rank ASC, underlying ASC`,
		turnoverIntersectionPoolTable,
		clickhouseStringLiteral(market),
		clickhouseBoolLiteral(nonETFOnly),
		clickhouseIntListLiteral(lookbackDays),
		clickhouseStringLiteral(from.Format("2006-01-02")),
		clickhouseStringLiteral(to.Format("2006-01-02")),
		clickhouseUInt32Literal(limit),
	)
	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query turnover intersection source membership: %w", err)
	}
	defer rows.Close()

	compressor := newUniverseMembershipCompressor()
	var currentDay time.Time
	currentMembers := make([]dto.UniverseMember, 0, limit*len(lookbackDays))
	flush := func() {
		if currentDay.IsZero() {
			return
		}
		compressor.AddDay(currentDay, currentMembers)
		currentMembers = currentMembers[:0]
	}
	for rows.Next() {
		var day time.Time
		var symbol string
		var score float64
		var rank uint32
		if err := rows.Scan(&day, &symbol, &score, &rank); err != nil {
			return nil, fmt.Errorf("scan turnover intersection source membership: %w", err)
		}
		day = normalizeUniverseDate(day, day)
		if currentDay.IsZero() {
			currentDay = day
		} else if !currentDay.Equal(day) {
			flush()
			currentDay = day
		}
		scoreCopy := score
		rankCopy := rank
		currentMembers = append(currentMembers, dto.UniverseMember{UniverseCode: code, Market: market, Symbol: normalizeSymbol(symbol), ValidFrom: day, ValidTo: day.AddDate(0, 0, 1), Score: &scoreCopy, Rank: &rankCopy, Source: string(dto.UniverseSourceTurnoverIntersectionUnion)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate turnover intersection source membership: %w", err)
	}
	flush()
	return compressor.Finish(to), nil
}

// ReplaceMembers overwrites universe membership for the given [from, to)
// window. Any existing membership interval that overlaps the window but
// extends beyond it is clipped and reinserted rather than dropped, so a
// narrow rebuild cannot silently erase validity outside the requested range.
// Calls for the same (market, code) are serialized to avoid interleaved
// delete/insert operations from concurrent rebuild requests.
func (s *UniverseService) ReplaceMembers(ctx context.Context, market, code string, from, to time.Time, members []dto.UniverseMember) error {
	unlock := s.lockUniverse(market, code)
	defer unlock()

	normMarket := normalizeUniverseMarket(market)
	normCode := normalizeUniverseCode(code)
	normFrom := normalizeUniverseDate(from, from)
	normTo := normalizeUniverseDate(to, to)

	existing, err := s.MemberIntervals(ctx, dto.UniverseMembersRequest{
		Market: normMarket,
		Code:   normCode,
		From:   normFrom,
		To:     normTo,
		Limit:  maxUniverseIntervalMembers,
	})
	if err != nil {
		return fmt.Errorf("replace universe members: fetch overlapping rows: %w", err)
	}
	remainders := clipUniverseMemberRemainders(existing.Data, normFrom, normTo)

	deleteQuery := fmt.Sprintf(`
ALTER TABLE universe_membership DELETE
WHERE market = %s
	AND universe_code = %s
	AND valid_from < toDate(%s)
	AND valid_to > toDate(%s)
SETTINGS mutations_sync = 1`,
		clickhouseStringLiteral(normMarket),
		clickhouseStringLiteral(normCode),
		clickhouseStringLiteral(normTo.Format("2006-01-02")),
		clickhouseStringLiteral(normFrom.Format("2006-01-02")),
	)
	if err := s.repo.Exec(ctx, deleteQuery); err != nil {
		return fmt.Errorf("replace universe members: delete existing rows: %w", err)
	}
	if err := s.InsertMembers(ctx, append(append([]dto.UniverseMember(nil), members...), remainders...)); err != nil {
		return fmt.Errorf("replace universe members: insert latest rows: %w", err)
	}
	return nil
}

// clipUniverseMemberRemainders returns the portions of existing membership
// intervals that fall outside [from, to). ReplaceMembers deletes every row
// that overlaps [from, to) before inserting the caller-provided members, so
// without this step any existing interval that extends before from or after
// to would lose that outside-the-window validity entirely. The head/tail
// segments returned here preserve it by reinserting clipped copies.
func clipUniverseMemberRemainders(existing []dto.UniverseMember, from, to time.Time) []dto.UniverseMember {
	remainders := make([]dto.UniverseMember, 0, len(existing))
	for _, member := range existing {
		if member.ValidFrom.Before(from) {
			head := member
			head.ValidTo = from
			if head.ValidTo.After(head.ValidFrom) {
				remainders = append(remainders, head)
			}
		}
		if member.ValidTo.After(to) {
			tail := member
			tail.ValidFrom = to
			if tail.ValidTo.After(tail.ValidFrom) {
				remainders = append(remainders, tail)
			}
		}
	}
	return remainders
}

type universeMembershipCompressor struct {
	open map[string]int
	out  []dto.UniverseMember
}

func newUniverseMembershipCompressor() *universeMembershipCompressor {
	return &universeMembershipCompressor{open: make(map[string]int), out: make([]dto.UniverseMember, 0)}
}

func (c *universeMembershipCompressor) AddDay(day time.Time, members []dto.UniverseMember) {
	active := make(map[string]dto.UniverseMember, len(members))
	for _, member := range members {
		active[member.Symbol] = member
		if index, ok := c.open[member.Symbol]; ok {
			if universeMemberStateEqual(c.out[index], member) {
				c.out[index].ValidTo = day.AddDate(0, 0, 1)
				continue
			}
			c.out[index].ValidTo = day
			member.ValidFrom = day
			member.ValidTo = day.AddDate(0, 0, 1)
			c.out = append(c.out, member)
			c.open[member.Symbol] = len(c.out) - 1
			continue
		}
		member.ValidFrom = day
		member.ValidTo = day.AddDate(0, 0, 1)
		c.out = append(c.out, member)
		c.open[member.Symbol] = len(c.out) - 1
	}
	for symbol, index := range c.open {
		if _, ok := active[symbol]; ok {
			continue
		}
		c.out[index].ValidTo = day
		delete(c.open, symbol)
	}
}

func (c *universeMembershipCompressor) Finish(to time.Time) []dto.UniverseMember {
	for _, index := range c.open {
		c.out[index].ValidTo = to
	}
	sort.SliceStable(c.out, func(i, j int) bool {
		if c.out[i].ValidFrom.Equal(c.out[j].ValidFrom) {
			return c.out[i].Symbol < c.out[j].Symbol
		}
		return c.out[i].ValidFrom.Before(c.out[j].ValidFrom)
	})
	return c.out
}

func compressUniverseDailyMembers(daily map[time.Time][]dto.UniverseMember, to time.Time) []dto.UniverseMember {
	days := make([]time.Time, 0, len(daily))
	for day := range daily {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	compressor := newUniverseMembershipCompressor()
	for _, day := range days {
		compressor.AddDay(day, daily[day])
	}
	return compressor.Finish(to)
}

func universeMemberStateEqual(left, right dto.UniverseMember) bool {
	return left.UniverseCode == right.UniverseCode &&
		left.Market == right.Market &&
		left.Symbol == right.Symbol &&
		floatPtrEqual(left.Score, right.Score) &&
		uint32PtrEqual(left.Rank, right.Rank) &&
		left.Metadata == right.Metadata &&
		left.Source == right.Source
}

func floatPtrEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func uint32PtrEqual(left, right *uint32) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func (s *UniverseService) InsertMembers(ctx context.Context, members []dto.UniverseMember) error {
	if len(members) == 0 {
		return nil
	}
	batch, err := s.repo.PrepareBatch(ctx, `INSERT INTO universe_membership (
		universe_code, market, symbol, valid_from, valid_to, score, rank, source_run_id, metadata, source, version, ingested_at
	)`)
	if err != nil {
		return fmt.Errorf("prepare universe membership batch: %w", err)
	}
	version := uint64(s.now().UnixMilli())
	ingestedAt := s.now().UTC()
	for _, member := range members {
		if err := batch.Append(
			member.UniverseCode,
			member.Market,
			member.Symbol,
			member.ValidFrom,
			member.ValidTo,
			member.Score,
			member.Rank,
			member.SourceRunID,
			member.Metadata,
			member.Source,
			version,
			ingestedAt,
		); err != nil {
			_ = batch.Abort()
			return fmt.Errorf("append universe member %s: %w", member.Symbol, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send universe membership batch: %w", err)
	}
	return nil
}

func (s *UniverseService) persistUniverseRun(ctx context.Context, code, market string, sourceType dto.UniverseSourceType, from, to time.Time, lookbackDays []int, limit int, nonETFOnly bool, runID string, memberCount int) error {
	return s.persistUniverseRunStatus(ctx, code, market, sourceType, from, to, lookbackDays, limit, nonETFOnly, runID, memberCount, "success", nil)
}

func (s *UniverseService) persistUniverseRunStatus(ctx context.Context, code, market string, sourceType dto.UniverseSourceType, from, to time.Time, lookbackDays []int, limit int, nonETFOnly bool, runID string, memberCount int, status string, failure error) error {
	if s.definitions == nil {
		return nil
	}
	params := map[string]any{"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "lookback_days": lookbackDays, "limit": limit, "non_etf_only": nonETFOnly}
	paramsJSON, _ := json.Marshal(params)
	statsJSON, _ := json.Marshal(map[string]any{"member_count": memberCount})
	now := s.now().UTC()
	startedAt := now
	var completedAt *time.Time
	if status == "success" || status == "failed" {
		completedAt = &now
	}
	failureText := ""
	if failure != nil {
		failureText = failure.Error()
	}
	if err := s.definitions.UpsertDefinition(ctx, universerepo.Definition{Code: code, Market: market, SourceType: string(sourceType), Parameters: string(paramsJSON), Version: 1, Active: true}); err != nil {
		return fmt.Errorf("upsert universe definition: %w", err)
	}
	if err := s.definitions.UpsertRun(ctx, universerepo.Run{RunID: runID, DefinitionCode: code, Market: market, Version: 1, Status: status, FromDate: from.Format("2006-01-02"), ToDate: to.Format("2006-01-02"), ParamsHash: universeHash(paramsJSON), IdempotencyKey: runID, Stats: string(statsJSON), Error: failureText, StartedAt: &startedAt, CompletedAt: completedAt}); err != nil {
		return fmt.Errorf("upsert universe run: %w", err)
	}
	return nil
}

func normalizeUniverseMarket(market string) string {
	market = strings.TrimSpace(strings.ToLower(market))
	if market == "" {
		return defaultUniverseMarket
	}
	return market
}

func normalizeUniverseCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

func normalizeUniverseDate(value, fallback time.Time) time.Time {
	if value.IsZero() {
		value = fallback
	}
	return time.Date(value.UTC().Year(), value.UTC().Month(), value.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func normalizeUniverseLookbacks(values []int) []int {
	if len(values) == 0 {
		return append([]int(nil), observedUSStockPoolLookbackDays...)
	}
	seen := make(map[int]struct{}, len(values))
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value < 7 || value > 252 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return append([]int(nil), observedUSStockPoolLookbackDays...)
	}
	sort.Ints(out)
	return out
}

func universeRebuildRequestHash(req dto.UniverseRebuildRequest) (string, error) {
	lookbackDays := append([]int(nil), req.LookbackDays...)
	sort.Ints(lookbackDays)
	symbols := make([]string, 0, len(req.Symbols))
	for _, symbol := range req.Symbols {
		if normalized := normalizeSymbol(symbol); normalized != "" {
			symbols = append(symbols, normalized)
		}
	}
	sort.Strings(symbols)
	members := append([]dto.UniverseMember(nil), req.Members...)
	for index := range members {
		members[index].Symbol = normalizeSymbol(members[index].Symbol)
		members[index].UniverseCode = normalizeUniverseCode(members[index].UniverseCode)
		members[index].Market = normalizeUniverseMarket(members[index].Market)
		members[index].ValidFrom = normalizeUniverseDate(members[index].ValidFrom, time.Time{})
		members[index].ValidTo = normalizeUniverseDate(members[index].ValidTo, time.Time{})
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].Symbol == members[j].Symbol {
			if members[i].ValidFrom.Equal(members[j].ValidFrom) {
				return members[i].ValidTo.Before(members[j].ValidTo)
			}
			return members[i].ValidFrom.Before(members[j].ValidFrom)
		}
		return members[i].Symbol < members[j].Symbol
	})
	payload := struct {
		Market             string                 `json:"market"`
		Code               string                 `json:"code"`
		SourceType         dto.UniverseSourceType `json:"source_type"`
		ForceRefresh       bool                   `json:"force_refresh"`
		ForceRebuildSource bool                   `json:"force_rebuild_source"`
		Symbols            []string               `json:"symbols,omitempty"`
		Members            []dto.UniverseMember   `json:"members,omitempty"`
		LookbackDays       []int                  `json:"lookback_days,omitempty"`
		Limit              int                    `json:"limit,omitempty"`
		NonETFOnly         *bool                  `json:"non_etf_only,omitempty"`
		DryRun             bool                   `json:"dry_run,omitempty"`
	}{
		Market:             normalizeUniverseMarket(req.Market),
		Code:               normalizeUniverseCode(req.Code),
		SourceType:         req.SourceType,
		ForceRefresh:       req.ForceRefresh,
		ForceRebuildSource: req.ForceRebuildSource,
		Symbols:            symbols,
		Members:            members,
		LookbackDays:       lookbackDays,
		Limit:              req.Limit,
		NonETFOnly:         req.NonETFOnly,
		DryRun:             req.DryRun,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("hash universe rebuild request: %w", err)
	}
	return universeHash(data), nil
}

func clickhouseIntListLiteral(values []int) string {
	if len(values) == 0 {
		return "0"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, clickhouseUInt32Literal(value))
	}
	return strings.Join(parts, ",")
}

func universeRunID(code, market string, sourceType dto.UniverseSourceType, from, to time.Time, members []dto.UniverseMember) string {
	parts := make([]string, 0, len(members)+3)
	parts = append(parts, code, market, string(sourceType), from.Format("2006-01-02"), to.Format("2006-01-02"))
	for _, member := range members {
		score := ""
		if member.Score != nil {
			score = fmt.Sprintf("%.6f", *member.Score)
		}
		rank := ""
		if member.Rank != nil {
			rank = fmt.Sprintf("%d", *member.Rank)
		}
		parts = append(parts, strings.Join([]string{member.Symbol, member.ValidFrom.Format("2006-01-02"), member.ValidTo.Format("2006-01-02"), score, rank, member.Metadata, member.Source}, ":"))
	}
	return universeHash([]byte(strings.Join(parts, "|")))[:24]
}

func universeHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
