package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/Cyvadra/toktik/internal/dto"
)

const RunManifestFileName = "run.json"

type RunManifest struct {
	Version   int                            `json:"version"`
	DSLSHA256 string                         `json:"dsl_sha256,omitempty"`
	EngineSHA string                         `json:"engine_sha,omitempty"`
	Status    *dto.StrategyBacktestRunStatus `json:"status"`
}

func NewRunManifest(status *dto.StrategyBacktestRunStatus) RunManifest {
	manifest := RunManifest{
		Version:   1,
		EngineSHA: EngineRevision(),
		Status:    status,
	}
	if status != nil && strings.TrimSpace(status.Request.DSL) != "" {
		manifest.DSLSHA256 = DSLHash(status.Request.DSL)
	}
	return manifest
}

func DSLHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

var (
	engineRevisionOnce sync.Once
	engineRevision     string
)

func EngineRevision() string {
	engineRevisionOnce.Do(func() {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				engineRevision = strings.TrimSpace(setting.Value)
				return
			}
		}
	})
	return engineRevision
}

func WriteRunManifest(dir string, manifest RunManifest) error {
	if manifest.Status == nil || strings.TrimSpace(manifest.Status.RunID) == "" {
		return fmt.Errorf("run manifest requires status with run id")
	}
	if manifest.Version <= 0 {
		manifest.Version = 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create run manifest directory: %w", err)
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run manifest: %w", err)
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(dir, ".run-*.json")
	if err != nil {
		return fmt.Errorf("create temporary run manifest: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set run manifest permissions: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write run manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync run manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close run manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(dir, RunManifestFileName)); err != nil {
		return fmt.Errorf("replace run manifest: %w", err)
	}
	return nil
}

func ReadRunManifest(dir string) (RunManifest, error) {
	payload, err := os.ReadFile(filepath.Join(dir, RunManifestFileName))
	if err != nil {
		return RunManifest{}, fmt.Errorf("read run manifest: %w", err)
	}
	var manifest RunManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return RunManifest{}, fmt.Errorf("decode run manifest: %w", err)
	}
	if manifest.Version != 1 {
		return RunManifest{}, fmt.Errorf("unsupported run manifest version %d", manifest.Version)
	}
	if manifest.Status == nil || strings.TrimSpace(manifest.Status.RunID) == "" {
		return RunManifest{}, fmt.Errorf("run manifest is missing status or run id")
	}
	return manifest, nil
}
