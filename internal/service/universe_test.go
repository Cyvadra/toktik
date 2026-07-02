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
	svc := NewUniverseService(nil, nil)
	resp, err := svc.Rebuild(context.Background(), dto.UniverseRebuildRequest{
		Market:     "us-stocks",
		Code:       "manual_watchlist",
		SourceType: dto.UniverseSourcePresetSymbols,
		From:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		Symbols:    []string{"msft", "AAPL", "MSFT"},
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Rebuild returned error: %v", err)
	}
	if resp.SourceType != dto.UniverseSourcePresetSymbols || resp.MemberCount != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if got := []string{resp.Data[0].Symbol, resp.Data[1].Symbol}; got[0] != "AAPL" || got[1] != "MSFT" {
		t.Fatalf("symbols = %v, want AAPL/MSFT", got)
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
