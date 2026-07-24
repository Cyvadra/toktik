package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/universerepo"
)

type stubUniverseDefinitions struct {
	definition universerepo.Definition
	run        universerepo.Run
}

func (s *stubUniverseDefinitions) UpsertDefinition(_ context.Context, definition universerepo.Definition) error {
	s.definition = definition
	return nil
}

func (s *stubUniverseDefinitions) UpsertRun(_ context.Context, run universerepo.Run) error {
	s.run = run
	return nil
}

func TestCompressUniverseDailyMembersCreatesValidityIntervals(t *testing.T) {
	jan01 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	jan02 := jan01.AddDate(0, 0, 1)
	jan03 := jan01.AddDate(0, 0, 2)
	jan04 := jan01.AddDate(0, 0, 3)
	daily := map[time.Time][]dto.UniverseMember{
		jan01: {{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "AAPL", ValidFrom: jan01, ValidTo: jan02}},
		jan02: {{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "AAPL", ValidFrom: jan02, ValidTo: jan03}, {UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "NVDA", ValidFrom: jan02, ValidTo: jan03}},
		jan03: {{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "NVDA", ValidFrom: jan03, ValidTo: jan04}},
	}

	members := compressUniverseDailyMembers(daily, jan04)
	if len(members) != 2 {
		t.Fatalf("len(members) = %d, want 2: %+v", len(members), members)
	}
	bySymbol := map[string]dto.UniverseMember{}
	for _, member := range members {
		bySymbol[member.Symbol] = member
	}
	if got := bySymbol["AAPL"]; !got.ValidFrom.Equal(jan01) || !got.ValidTo.Equal(jan03) {
		t.Fatalf("AAPL interval = %s..%s, want %s..%s", got.ValidFrom, got.ValidTo, jan01, jan03)
	}
	if got := bySymbol["NVDA"]; !got.ValidFrom.Equal(jan02) || !got.ValidTo.Equal(jan04) {
		t.Fatalf("NVDA interval = %s..%s, want %s..%s", got.ValidFrom, got.ValidTo, jan02, jan04)
	}
}

func TestCompressUniverseDailyMembersSplitsWhenPointInTimeStateChanges(t *testing.T) {
	jan01 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	jan02 := jan01.AddDate(0, 0, 1)
	jan03 := jan01.AddDate(0, 0, 2)
	score1 := 100.0
	score2 := 200.0
	rank1 := uint32(1)
	rank2 := uint32(2)
	daily := map[time.Time][]dto.UniverseMember{
		jan01: {{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "AAPL", ValidFrom: jan01, ValidTo: jan02, Score: &score1, Rank: &rank1, Source: "test"}},
		jan02: {{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "AAPL", ValidFrom: jan02, ValidTo: jan03, Score: &score2, Rank: &rank2, Source: "test"}},
	}

	members := compressUniverseDailyMembers(daily, jan03)
	if len(members) != 2 {
		t.Fatalf("len(members) = %d, want 2: %+v", len(members), members)
	}
	if got := members[0]; !got.ValidFrom.Equal(jan01) || !got.ValidTo.Equal(jan02) || got.Score == nil || *got.Score != score1 || got.Rank == nil || *got.Rank != rank1 {
		t.Fatalf("first interval leaked state: %+v", got)
	}
	if got := members[1]; !got.ValidFrom.Equal(jan02) || !got.ValidTo.Equal(jan03) || got.Score == nil || *got.Score != score2 || got.Rank == nil || *got.Rank != rank2 {
		t.Fatalf("second interval = %+v, want Jan02..Jan03 score/rank 2", got)
	}
}

func TestPersistUniverseRunRecordsEffectiveParameters(t *testing.T) {
	defs := &stubUniverseDefinitions{}
	svc := NewUniverseService(nil, defs)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	if err := svc.persistUniverseRun(context.Background(), "strong_momentum", "us-stocks", dto.UniverseSourceTurnoverIntersectionUnion, from, to, []int{20, 60}, 17, false, "run-1", 9); err != nil {
		t.Fatalf("persistUniverseRun returned error: %v", err)
	}
	if defs.run.FromDate != "2024-01-01" || defs.run.ToDate != "2024-02-01" {
		t.Fatalf("run dates = %s..%s, want 2024-01-01..2024-02-01", defs.run.FromDate, defs.run.ToDate)
	}
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(defs.definition.Parameters), &params); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	if params["limit"] != float64(17) || params["non_etf_only"] != false || params["from"] != "2024-01-01" || params["to"] != "2024-02-01" {
		t.Fatalf("unexpected params: %+v", params)
	}
}

func TestPersistUniverseRunStatusRecordsFailedError(t *testing.T) {
	defs := &stubUniverseDefinitions{}
	svc := NewUniverseService(nil, defs)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	err := svc.persistUniverseRunStatus(context.Background(), "strong_momentum", "us-stocks", dto.UniverseSourcePresetSymbols, from, to, nil, 2, true, "run-failed", 2, "failed", errors.New("clickhouse write failed"))
	if err != nil {
		t.Fatalf("persistUniverseRunStatus returned error: %v", err)
	}
	if defs.run.Status != "failed" || defs.run.Error != "clickhouse write failed" {
		t.Fatalf("run status/error = %q/%q, want failed/clickhouse write failed", defs.run.Status, defs.run.Error)
	}
	if defs.run.CompletedAt == nil {
		t.Fatalf("failed run should record completed_at")
	}
}

func TestRebuildPresetSymbolsDryRun(t *testing.T) {
	var req dto.UniverseRebuildRequest
	if err := json.Unmarshal([]byte(`{"market":"us-stocks","code":"manual_watchlist","source_type":"preset_symbols","symbols":["msft","AAPL","MSFT"],"dry_run":true}`), &req); err != nil {
		t.Fatalf("unmarshal rebuild request: %v", err)
	}
	if !req.ForceRefresh {
		t.Fatal("force_refresh should default to true")
	}
	if req.SourceType != dto.UniverseSourcePresetSymbols || len(req.Symbols) != 3 || !req.DryRun {
		t.Fatalf("unexpected request: %+v", req)
	}
}

func TestSelectUniverseRebuildWindow(t *testing.T) {
	start := time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	validTo := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	from, to, noWork := selectUniverseRebuildWindow(start, latest, validTo, true, true)
	if !from.Equal(start) || !to.Equal(latest.AddDate(0, 0, 1)) || noWork {
		t.Fatalf("force-refresh window = %s..%s noWork=%t, want %s..%s false", from, to, noWork, start, latest.AddDate(0, 0, 1))
	}

	from, to, noWork = selectUniverseRebuildWindow(start, latest, validTo, true, false)
	if !from.Equal(validTo) || !to.Equal(latest.AddDate(0, 0, 1)) || noWork {
		t.Fatalf("incremental window = %s..%s noWork=%t, want %s..%s false", from, to, noWork, validTo, latest.AddDate(0, 0, 1))
	}

	from, to, noWork = selectUniverseRebuildWindow(start, latest, latest.AddDate(0, 0, 1), true, false)
	if !noWork || !from.Equal(to) {
		t.Fatalf("up-to-date incremental window = %s..%s noWork=%t, want empty no-op", from, to, noWork)
	}
}

func TestMinimumUniverseReferenceDateUsesEarlierSPYFeed(t *testing.T) {
	stockLatest := time.Date(2026, 7, 23, 15, 30, 0, 0, time.UTC)
	optionsLatest := time.Date(2026, 7, 22, 19, 0, 0, 0, time.UTC)
	if got := minimumUniverseReferenceDate(stockLatest, optionsLatest); !got.Equal(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("reference latest = %s, want 2026-07-22", got)
	}
}

func TestNormalizeStaticUniverseMembersIntersectsRequestedRange(t *testing.T) {
	from := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)
	members := normalizeStaticUniverseMembers(dto.UniverseRebuildRequest{Members: []dto.UniverseMember{
		{Symbol: "AAPL", ValidFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), ValidTo: time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)},
		{Symbol: "MSFT", ValidFrom: time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC), ValidTo: time.Date(2024, 1, 25, 0, 0, 0, 0, time.UTC)},
	}}, "us-stocks", "manual", dto.UniverseSourcePresetSymbols, from, to)

	if len(members) != 1 {
		t.Fatalf("len(members) = %d, want 1: %+v", len(members), members)
	}
	if got := members[0]; got.Symbol != "AAPL" || !got.ValidFrom.Equal(from) || !got.ValidTo.Equal(time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("member = %+v, want AAPL clipped to 2024-01-10..2024-01-12", got)
	}
}

func TestUniverseRunIDIncludesRangeAndRank(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	rank1 := uint32(1)
	rank2 := uint32(2)
	members := []dto.UniverseMember{{Symbol: "AAPL", ValidFrom: from, ValidTo: to, Rank: &rank1, Source: string(dto.UniverseSourcePresetSymbols)}}

	base := universeRunID("manual", "us-stocks", dto.UniverseSourcePresetSymbols, from, to, members)
	if got := universeRunID("manual", "us-stocks", dto.UniverseSourcePresetSymbols, from, to.AddDate(0, 0, 1), members); got == base {
		t.Fatalf("run id should change when rebuild range changes")
	}
	members[0].Rank = &rank2
	if got := universeRunID("manual", "us-stocks", dto.UniverseSourcePresetSymbols, from, to, members); got == base {
		t.Fatalf("run id should change when member rank changes")
	}
}

func TestClipUniverseMemberRemaindersPreservesOutOfWindowValidity(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	beforeWindow := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	afterWindow := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	tailStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	tailEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	existing := []dto.UniverseMember{
		// Spans the entire rebuild window on both sides: must keep a head
		// segment before `from` and a tail segment after `to`.
		{Symbol: "AAPL", ValidFrom: beforeWindow, ValidTo: afterWindow},
		// Fully inside the window: superseded entirely, no remainder.
		{Symbol: "MSFT", ValidFrom: from, ValidTo: to},
		// Starts inside the window but extends past `to`: tail-only remainder.
		{Symbol: "NVDA", ValidFrom: tailStart, ValidTo: tailEnd},
	}

	remainders := clipUniverseMemberRemainders(existing, from, to)

	bySymbol := map[string][]dto.UniverseMember{}
	for _, remainder := range remainders {
		bySymbol[remainder.Symbol] = append(bySymbol[remainder.Symbol], remainder)
	}

	aapl := bySymbol["AAPL"]
	if len(aapl) != 2 {
		t.Fatalf("AAPL remainders = %+v, want head+tail", aapl)
	}
	var aaplHead, aaplTail dto.UniverseMember
	for _, remainder := range aapl {
		if remainder.ValidFrom.Equal(beforeWindow) {
			aaplHead = remainder
		} else {
			aaplTail = remainder
		}
	}
	if !aaplHead.ValidFrom.Equal(beforeWindow) || !aaplHead.ValidTo.Equal(from) {
		t.Fatalf("AAPL head remainder = %+v, want %s..%s", aaplHead, beforeWindow, from)
	}
	if !aaplTail.ValidFrom.Equal(to) || !aaplTail.ValidTo.Equal(afterWindow) {
		t.Fatalf("AAPL tail remainder = %+v, want %s..%s", aaplTail, to, afterWindow)
	}

	if _, ok := bySymbol["MSFT"]; ok {
		t.Fatalf("MSFT is fully inside the rebuild window and should have no remainder, got %+v", bySymbol["MSFT"])
	}

	nvda := bySymbol["NVDA"]
	if len(nvda) != 1 || !nvda[0].ValidFrom.Equal(to) || !nvda[0].ValidTo.Equal(tailEnd) {
		t.Fatalf("NVDA tail remainder = %+v, want single %s..%s", nvda, to, tailEnd)
	}
}

func TestClipUniverseMemberRemaindersDropsDegenerateSegments(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	existing := []dto.UniverseMember{
		// ValidFrom == from and ValidTo == to: exactly the window, no remainder.
		{Symbol: "AAPL", ValidFrom: from, ValidTo: to},
	}

	remainders := clipUniverseMemberRemainders(existing, from, to)
	if len(remainders) != 0 {
		t.Fatalf("remainders = %+v, want none for an interval exactly matching the window", remainders)
	}
}

func TestLockUniverseSerializesSameKey(t *testing.T) {
	svc := NewUniverseService(nil, nil)
	unlockFirst := svc.lockUniverse("us-stocks", "strong_momentum")

	acquired := make(chan struct{})
	go func() {
		unlock := svc.lockUniverse("US-Stocks", "Strong_Momentum") // case-insensitive: same key
		close(acquired)
		unlock()
	}()

	select {
	case <-acquired:
		t.Fatal("second lockUniverse call acquired the lock while the first holder still held it")
	case <-time.After(50 * time.Millisecond):
	}

	unlockFirst()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second lockUniverse call never acquired the lock after the first holder released it")
	}
}

func TestLockUniverseAllowsDifferentKeysConcurrently(t *testing.T) {
	svc := NewUniverseService(nil, nil)
	unlockA := svc.lockUniverse("us-stocks", "strong_momentum")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlock := svc.lockUniverse("us-stocks", "value_allocation")
		unlock()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lockUniverse for an unrelated (market, code) key blocked unexpectedly")
	}
}
