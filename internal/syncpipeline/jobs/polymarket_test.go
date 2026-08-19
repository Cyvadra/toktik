package jobs

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/polymarket"
)

func TestParsePolymarketFileHour(t *testing.T) {
	got, err := parsePolymarketFileHour("/raw/polymarket_orderbook_2026-08-08T05.parquet")
	if err != nil {
		t.Fatalf("parse file hour: %v", err)
	}
	want := time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("file hour = %s, want %s", got, want)
	}
}

func TestPlanPolymarketArchiveIncludesWarmupAndWritableFiles(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC)
	for offset := -50; offset < 3; offset++ {
		name := "polymarket_orderbook_" + start.Add(time.Duration(offset)*time.Hour).Format("2006-01-02T15") + ".parquet"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{byte(offset)}, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
	}
	plan, err := planPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:     dir,
		ArchiveFrom: start,
		ArchiveTo:   start.Add(2 * time.Hour),
	}, 49*time.Hour)
	if err != nil {
		t.Fatalf("plan archive: %v", err)
	}
	if len(plan.Files) != 51 || plan.WritableFrom != 49 {
		t.Fatalf("plan files = %d writable_from = %d, want 51 and 49", len(plan.Files), plan.WritableFrom)
	}
	if !plan.Files[0].Hour.Equal(start.Add(-49*time.Hour)) || !plan.Files[0].Warmup {
		t.Fatalf("first warmup file = %+v", plan.Files[0])
	}
	if !plan.Files[plan.WritableFrom].Hour.Equal(start) || plan.Files[plan.WritableFrom].Warmup {
		t.Fatalf("first writable file = %+v", plan.Files[plan.WritableFrom])
	}
	if plan.ManifestHash == "" {
		t.Fatal("manifest hash must not be empty")
	}
}

func TestPlanPolymarketArchiveLimitAppliesOnlyToWritableFiles(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC)
	for offset := -2; offset < 3; offset++ {
		name := "polymarket_orderbook_" + start.Add(time.Duration(offset)*time.Hour).Format("2006-01-02T15") + ".parquet"
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
	}
	plan, err := planPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, ArchiveFrom: start, LimitFiles: 1}, 49*time.Hour)
	if err != nil {
		t.Fatalf("plan archive: %v", err)
	}
	if len(plan.Files) != 3 || plan.WritableFrom != 2 || !plan.Files[2].Hour.Equal(start) {
		t.Fatalf("unexpected limited plan: %+v", plan)
	}
}

func TestDirtyPolymarketFilesWarmsBeforeEarliestDirtyAndReprocessesTail(t *testing.T) {
	start := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	plan := polymarketArchivePlan{WritableFrom: 2}
	checkpoints := make(map[string]polymarket.RawFileCheckpoint)
	for index := 0; index < 6; index++ {
		name := "file-" + string(rune('0'+index))
		plan.Files = append(plan.Files, polymarketArchiveFile{Name: name, Hour: start.Add(time.Duration(index) * time.Hour), Fingerprint: "fp"})
		if index >= plan.WritableFrom {
			checkpoints[name] = polymarket.RawFileCheckpoint{SourceHash: "fp", SelectionHash: "map", SchemaVersion: 2, Status: "success"}
		}
	}
	checkpoints["file-4"] = polymarket.RawFileCheckpoint{SourceHash: "changed", SelectionHash: "map", SchemaVersion: 2, Status: "success"}
	files := dirtyPolymarketFiles(plan, checkpoints, "map", 2, 49*time.Hour)
	if len(files) != 6 || !files[0].Warmup || !files[3].Warmup || files[4].Warmup || files[4].Name != "file-4" || !files[4].Replace || files[5].Name != "file-5" || !files[5].Replace {
		t.Fatalf("unexpected dirty files: %+v", files)
	}
}

func TestDirtyPolymarketFilesDoesNotReplaceFreshFiles(t *testing.T) {
	start := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	plan := polymarketArchivePlan{Files: []polymarketArchiveFile{
		{Name: "fresh", Hour: start, Fingerprint: "fp"},
		{Name: "failed", Hour: start.Add(time.Hour), Fingerprint: "fp"},
	}}
	checkpoints := map[string]polymarket.RawFileCheckpoint{
		"failed": {SourceHash: "fp", SelectionHash: "map", SchemaVersion: 2, Status: "failed"},
	}
	files := dirtyPolymarketFiles(plan, checkpoints, "map", 2, 49*time.Hour)
	if len(files) != 2 || files[0].Replace || !files[1].Replace {
		t.Fatalf("unexpected replacement flags: %+v", files)
	}
}

func TestDirtyPolymarketFilesRetriesSkippedFiles(t *testing.T) {
	plan := polymarketArchivePlan{Files: []polymarketArchiveFile{{
		Name:        "corrupt",
		Hour:        time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		Fingerprint: "fp",
	}}}
	checkpoints := map[string]polymarket.RawFileCheckpoint{
		"corrupt": {SourceHash: "fp", SelectionHash: "map", SchemaVersion: 2, Status: "skipped"},
	}
	files := dirtyPolymarketFiles(plan, checkpoints, "map", 2, 49*time.Hour)
	if len(files) != 1 || files[0].Name != "corrupt" || !files[0].Replace {
		t.Fatalf("dirty files = %+v, want corrupt file retry", files)
	}
}

func TestDirtyPolymarketFilesReturnsNoneWhenAllWritableFilesMatch(t *testing.T) {
	plan := polymarketArchivePlan{WritableFrom: 1, Files: []polymarketArchiveFile{
		{Name: "warmup", Hour: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)},
		{Name: "current", Hour: time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC), Fingerprint: "fp"},
	}}
	checkpoints := map[string]polymarket.RawFileCheckpoint{"current": {SourceHash: "fp", SelectionHash: "map", SchemaVersion: 2, Status: "success"}}
	if files := dirtyPolymarketFiles(plan, checkpoints, "map", 2, 49*time.Hour); len(files) != 0 {
		t.Fatalf("dirty files = %+v, want none", files)
	}
}

func TestPolymarketArchiveSourceKeysReturnsSingleArchiveAndRespectsLimit(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	for _, name := range []string{"polymarket_orderbook_2026-08-08T06.parquet", "polymarket_orderbook_2026-08-08T05.parquet"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, ConditionMap: conditionMap, LimitFiles: 1})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("source keys: %v", err)
	}
	if len(keys) != 1 || !strings.HasPrefix(keys[0], polymarketArchiveSourcePrefix) {
		t.Fatalf("unexpected source keys: %v", keys)
	}
}

func TestPolymarketArchiveSourceKeysFilterHourRange(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	for _, hour := range []string{"04", "05", "06", "07"} {
		name := "polymarket_orderbook_2026-08-08T" + hour + ".parquet"
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:      dir,
		ConditionMap: conditionMap,
		ArchiveFrom:  time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC),
		ArchiveTo:    time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("source keys: %v", err)
	}
	if len(keys) != 1 || !strings.HasPrefix(keys[0], polymarketArchiveSourcePrefix) {
		t.Fatalf("unexpected ranged source keys: %v", keys)
	}
}

func TestPolymarketArchiveSourceKeysAreStableAcrossRawRootChanges(t *testing.T) {
	conditionMap := filepath.Join(t.TempDir(), "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	var keys []string
	modified := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	for _, root := range []string{t.TempDir(), t.TempDir()} {
		parquetPath := filepath.Join(root, "polymarket_orderbook_2026-08-08T05.parquet")
		if err := os.WriteFile(parquetPath, nil, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
		if err := os.Chtimes(parquetPath, modified, modified); err != nil {
			t.Fatalf("set parquet timestamp: %v", err)
		}
		syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: root, ConditionMap: conditionMap})
		if err != nil {
			t.Fatalf("create syncer: %v", err)
		}
		resolved, err := syncer.SourceKeys(context.Background(), nil)
		if err != nil {
			t.Fatalf("source keys: %v", err)
		}
		keys = append(keys, resolved[0])
	}
	if keys[0] != keys[1] {
		t.Fatalf("source key changed after raw root move: %q != %q", keys[0], keys[1])
	}
}

func TestPolymarketArchiveSourceKeyChangesWithFileMetadata(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	parquetPath := filepath.Join(dir, "polymarket_orderbook_2026-08-08T05.parquet")
	if err := os.WriteFile(parquetPath, []byte("first"), 0o600); err != nil {
		t.Fatalf("write first parquet: %v", err)
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, ConditionMap: conditionMap})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	first, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("first source keys: %v", err)
	}
	if err := os.WriteFile(parquetPath, []byte("corrected archive file"), 0o600); err != nil {
		t.Fatalf("write corrected parquet: %v", err)
	}
	second, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("second source keys: %v", err)
	}
	if first[0] == second[0] {
		t.Fatalf("source key must change after immutable file replacement: %q", first[0])
	}
}

func TestPolymarketArchiveSourceKeyChangesWhenSameSizeFileIsReplaced(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	parquetPath := filepath.Join(dir, "polymarket_orderbook_2026-08-08T05.parquet")
	if err := os.WriteFile(parquetPath, []byte("first"), 0o600); err != nil {
		t.Fatalf("write first parquet: %v", err)
	}
	modified := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(parquetPath, modified, modified); err != nil {
		t.Fatalf("set first parquet timestamp: %v", err)
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, ConditionMap: conditionMap})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	first, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("first source keys: %v", err)
	}
	if err := os.WriteFile(parquetPath, []byte("other"), 0o600); err != nil {
		t.Fatalf("replace parquet: %v", err)
	}
	if err := os.Chtimes(parquetPath, modified, modified); err != nil {
		t.Fatalf("preserve parquet timestamp: %v", err)
	}
	second, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("second source keys: %v", err)
	}
	if first[0] == second[0] {
		t.Fatalf("source key must change after same-size file replacement: %q", first[0])
	}
}

func TestPolymarketArchiveUsesStableScopes(t *testing.T) {
	syncer := &polymarketArchive{conditionMapHash: "map-hash"}
	if scope, ok := syncer.StableScope("archive:key"); !ok || scope != "archive:map-hash" {
		t.Fatalf("unexpected archive stable scope: %q ok=%v", scope, ok)
	}
}

func TestPolymarketArchiveAuditsEventIDs(t *testing.T) {
	syncer := &polymarketArchive{}
	targets := syncer.AuditTargets("archive:key")
	if len(targets) != 1 || targets[0].Table != "polymarket_l2_event" || targets[0].DateColumn != "toDate(timestamp_received)" || len(targets[0].KeyColumns) != 1 || targets[0].KeyColumns[0] != "event_id" {
		t.Fatalf("unexpected audit targets: %+v", targets)
	}
}

func TestPolymarketArchiveIsNotAnIncrementalCursor(t *testing.T) {
	want := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	syncer := &polymarketArchive{cfg: PolymarketArchiveConfig{ArchiveFrom: want}}
	sourceKey := "archive:key"

	cursor, ok, err := syncer.ResolveCursor(context.Background(), nil, sourceKey)
	if err != nil {
		t.Fatalf("resolve cursor: %v", err)
	}
	if ok {
		t.Fatal("immutable archive file must not report an incremental cursor")
	}
	if !cursor.Equal(want) || !syncer.ColdStartFloor(sourceKey).Equal(want) {
		t.Fatalf("cursor = %s, floor = %s, want %s", cursor, syncer.ColdStartFloor(sourceKey), want)
	}
}

func TestPolymarketArchiveUsesSingleSourceConcurrency(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}

	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:      dir,
		ConditionMap: conditionMap,
	})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	if got := syncer.MaxConcurrency(); got != 1 {
		t.Fatalf("max concurrency = %d, want 1", got)
	}
	archive := syncer.(*polymarketArchive)
	if archive.cfg.WriterConcurrency != 2 {
		t.Fatalf("writer concurrency = %d, want default 2", archive.cfg.WriterConcurrency)
	}
}

func TestPolymarketArchiveDefaultsStageWorkersWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, StageRoot: filepath.Join(dir, "stage"), ConditionMap: conditionMap})
	if err != nil {
		t.Fatal(err)
	}
	if got := syncer.(*polymarketArchive).cfg.StageWorkers; got != 4 {
		t.Fatalf("stage workers = %d, want default 4", got)
	}
}

func TestPolymarketArchiveRejectsExcessiveStageWorkers(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, StageRoot: filepath.Join(dir, "stage"), StageWorkers: 17, ConditionMap: conditionMap}); err == nil || !strings.Contains(err.Error(), "stage_workers") {
		t.Fatalf("expected stage worker validation error, got %v", err)
	}
}

func TestPolymarketArchiveRejectsExcessiveWriterConcurrency(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	if _, err := NewPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:           dir,
		ConditionMap:      conditionMap,
		WriterConcurrency: 9,
	}); err == nil || !strings.Contains(err.Error(), "writer_concurrency") {
		t.Fatalf("expected writer concurrency validation error, got %v", err)
	}
}

func TestPolymarketArchiveFileLoggerEmitsBenchmarkFields(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	polymarketArchiveFileLogger(logger)(polymarket.ArchiveFileMetrics{
		Name:          "polymarket_orderbook_2026-08-08T05.parquet",
		Status:        polymarket.ArchiveFileImported,
		SizeBytes:     8 * 1024 * 1024,
		SourceRows:    1_000,
		SelectedRows:  250,
		WriterBatches: 2,
		WriterWait:    500 * time.Millisecond,
		Elapsed:       2 * time.Second,
		StageCacheHit: true,
		StageWait:     125 * time.Millisecond,
	})

	logged := output.String()
	for _, want := range []string{
		`"msg":"Polymarket archive file completed"`,
		`"status":"imported"`,
		`"mib_per_second":4`,
		`"rows_per_second":500`,
		`"writer_wait_ratio":0.25`,
		`"stage_cache_hit":true`,
		`"stage_wait":125000000`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %q missing %q", logged, want)
		}
	}
}
