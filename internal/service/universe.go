package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/universerepo"
)

const (
	defaultUniverseMarket      = "us-stocks"
	defaultUniverseLimit       = 5000
	maxUniverseMembers         = 5000
	maxUniverseIntervalMembers = 500000
)

type universeDefinitionRepo interface {
	UpsertDefinition(ctx context.Context, definition universerepo.Definition) error
	UpsertRun(ctx context.Context, run universerepo.Run) error
}

type UniverseService struct {
	repo        *chrepo.Repo
	definitions universeDefinitionRepo
	screener    usTurnoverIntersectionScreener
	now         func() time.Time
}

type UniverseIntervalProvider struct {
	members map[string][]dto.UniverseMember
}

func NewUniverseService(repo *chrepo.Repo, definitions universeDefinitionRepo) *UniverseService {
	return &UniverseService{repo: repo, definitions: definitions, now: time.Now}
}

func (s *UniverseService) WithTurnoverScreener(screener usTurnoverIntersectionScreener) *UniverseService {
	if s == nil {
		return nil
	}
	s.screener = screener
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

func (s *UniverseService) LoadProvider(ctx context.Context, req dto.UniverseMembersRequest, codes []string) (*UniverseIntervalProvider, error) {
	provider := &UniverseIntervalProvider{members: make(map[string][]dto.UniverseMember, len(codes))}
	for _, rawCode := range codes {
		code := normalizeUniverseCode(rawCode)
		if code == "" {
			continue
		}
		req.Code = code
		resp, err := s.MemberIntervals(ctx, req)
		if err != nil {
			return nil, err
		}
		members := append([]dto.UniverseMember(nil), resp.Data...)
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].ValidFrom.Equal(members[j].ValidFrom) {
				if members[i].Rank == nil || members[j].Rank == nil || *members[i].Rank == *members[j].Rank {
					return members[i].Symbol < members[j].Symbol
				}
				return *members[i].Rank < *members[j].Rank
			}
			return members[i].ValidFrom.Before(members[j].ValidFrom)
		})
		provider.members[code] = members
	}
	return provider, nil
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

func (s *UniverseService) Rebuild(ctx context.Context, req dto.UniverseRebuildRequest) (*dto.UniverseRebuildResponse, error) {
	market := normalizeUniverseMarket(req.Market)
	code := normalizeUniverseCode(req.Code)
	if code == "" {
		return nil, dto.NewValidationError("universe code must be non-empty")
	}
	from, to, err := normalizeUniverseRebuildRange(req, s.now())
	if err != nil {
		return nil, err
	}
	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = dto.UniverseSourceTurnoverIntersectionUnion
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
	if s.screener == nil {
		return nil, fmt.Errorf("turnover intersection screener not configured")
	}
	lookbackDays := normalizeUniverseLookbacks(req.LookbackDays)
	limit := req.Limit
	if limit <= 0 {
		limit = observedUSStockPoolTopLimit
	}
	nonETFOnly := true
	if req.NonETFOnly != nil {
		nonETFOnly = *req.NonETFOnly
	}

	daily := make(map[time.Time][]dto.UniverseMember)
	for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
		members, err := s.buildTurnoverIntersectionMembersForDate(ctx, market, code, day, lookbackDays, limit, nonETFOnly)
		if err != nil {
			return nil, err
		}
		daily[day] = members
	}
	members := compressUniverseDailyMembers(daily, to)
	runID := universeRunID(code, market, dto.UniverseSourceTurnoverIntersectionUnion, from, to, members)
	for index := range members {
		members[index].SourceRunID = runID
	}
	if !req.DryRun {
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
	}
	return &dto.UniverseRebuildResponse{Market: market, Code: code, SourceType: dto.UniverseSourceTurnoverIntersectionUnion, AsOf: from, From: from, To: to, RunID: runID, DryRun: req.DryRun, MemberCount: len(members), LookbackDays: lookbackDays, Data: members}, nil
}

func (s *UniverseService) ReplaceMembers(ctx context.Context, market, code string, from, to time.Time, members []dto.UniverseMember) error {
	deleteQuery := fmt.Sprintf(`
ALTER TABLE universe_membership DELETE
WHERE market = %s
	AND universe_code = %s
	AND valid_from < toDate(%s)
	AND valid_to > toDate(%s)
SETTINGS mutations_sync = 1`,
		clickhouseStringLiteral(normalizeUniverseMarket(market)),
		clickhouseStringLiteral(normalizeUniverseCode(code)),
		clickhouseStringLiteral(normalizeUniverseDate(to, to).Format("2006-01-02")),
		clickhouseStringLiteral(normalizeUniverseDate(from, from).Format("2006-01-02")),
	)
	if err := s.repo.Exec(ctx, deleteQuery); err != nil {
		return fmt.Errorf("replace universe members: delete existing rows: %w", err)
	}
	if err := s.InsertMembers(ctx, members); err != nil {
		return fmt.Errorf("replace universe members: insert latest rows: %w", err)
	}
	return nil
}

func (s *UniverseService) buildTurnoverIntersectionMembersForDate(ctx context.Context, market, code string, asOf time.Time, lookbackDays []int, limit int, nonETFOnly bool) ([]dto.UniverseMember, error) {
	seen := make(map[string]struct{}, limit*len(lookbackDays))
	members := make([]dto.UniverseMember, 0, limit*len(lookbackDays))
	for _, lookbackDays := range lookbackDays {
		resp, err := s.screener.ScreenUSTurnoverIntersection(ctx, dto.ScreenUSTurnoverIntersectionRequest{Limit: limit, LookbackDays: lookbackDays, NonETFOnly: nonETFOnly, AsOf: asOf.Format("2006-01-02")})
		if err != nil {
			return nil, fmt.Errorf("screen turnover intersection for %d-day lookback at %s: %w", lookbackDays, asOf.Format("2006-01-02"), err)
		}
		for _, row := range resp.Data {
			symbol := normalizeSymbol(row.Underlying)
			if symbol == "" {
				continue
			}
			if _, ok := seen[symbol]; ok {
				continue
			}
			seen[symbol] = struct{}{}
			score := row.CombinedTurnoverUSD
			members = append(members, dto.UniverseMember{UniverseCode: code, Market: market, Symbol: symbol, ValidFrom: asOf, ValidTo: asOf.AddDate(0, 0, 1), Score: &score, Source: string(dto.UniverseSourceTurnoverIntersectionUnion)})
		}
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].Score == nil || members[j].Score == nil || *members[i].Score == *members[j].Score {
			return members[i].Symbol < members[j].Symbol
		}
		return *members[i].Score > *members[j].Score
	})
	for index := range members {
		rank := uint32(index + 1)
		members[index].Rank = &rank
	}
	return members, nil
}

func compressUniverseDailyMembers(daily map[time.Time][]dto.UniverseMember, to time.Time) []dto.UniverseMember {
	days := make([]time.Time, 0, len(daily))
	for day := range daily {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	open := make(map[string]int)
	out := make([]dto.UniverseMember, 0)
	for _, day := range days {
		active := make(map[string]dto.UniverseMember, len(daily[day]))
		for _, member := range daily[day] {
			active[member.Symbol] = member
			if index, ok := open[member.Symbol]; ok {
				if universeMemberStateEqual(out[index], member) {
					out[index].ValidTo = day.AddDate(0, 0, 1)
					continue
				}
				out[index].ValidTo = day
				member.ValidFrom = day
				member.ValidTo = day.AddDate(0, 0, 1)
				out = append(out, member)
				open[member.Symbol] = len(out) - 1
				continue
			}
			member.ValidFrom = day
			member.ValidTo = day.AddDate(0, 0, 1)
			out = append(out, member)
			open[member.Symbol] = len(out) - 1
		}
		for symbol, index := range open {
			if _, ok := active[symbol]; ok {
				continue
			}
			out[index].ValidTo = day
			delete(open, symbol)
		}
	}
	for _, index := range open {
		out[index].ValidTo = to
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ValidFrom.Equal(out[j].ValidFrom) {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].ValidFrom.Before(out[j].ValidFrom)
	})
	return out
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

func normalizeUniverseRebuildRange(req dto.UniverseRebuildRequest, fallback time.Time) (time.Time, time.Time, error) {
	from := req.From
	to := req.To
	if from.IsZero() && !req.AsOf.IsZero() {
		from = req.AsOf
	}
	from = normalizeUniverseDate(from, fallback)
	if to.IsZero() {
		if !req.To.IsZero() {
			to = req.To
		} else {
			to = from.AddDate(0, 0, 1)
		}
	}
	to = normalizeUniverseDate(to, from.AddDate(0, 0, 1))
	if !to.After(from) {
		return time.Time{}, time.Time{}, dto.NewValidationError("universe rebuild to must be after from")
	}
	return from, to, nil
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
