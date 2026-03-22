package cryptooptions

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestParseCSVFromZST_CompressedFormats(t *testing.T) {
	t.Parallel()

	const csvData = "symbol,timestamp\nBTC-3FEB24-42250-P,1706918400091000\n"
	expectedTime := time.Unix(1706918400, 91_000_000).UTC()

	tests := []struct {
		name  string
		path  string
		write func(string, []byte) error
	}{
		{
			name: "gzip",
			path: "ticks.csv.zst",
			write: func(path string, data []byte) error {
				file, err := os.Create(path)
				if err != nil {
					return err
				}
				defer file.Close()

				writer := gzip.NewWriter(file)
				if _, err := writer.Write(data); err != nil {
					writer.Close()
					return err
				}
				return writer.Close()
			},
		},
		{
			name: "zstd",
			path: "ticks.csv.zst",
			write: func(path string, data []byte) error {
				file, err := os.Create(path)
				if err != nil {
					return err
				}
				defer file.Close()

				writer, err := zstd.NewWriter(file)
				if err != nil {
					return err
				}
				if _, err := writer.Write(data); err != nil {
					writer.Close()
					return err
				}
				return writer.Close()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), tt.path)
			if err := tt.write(path, []byte(csvData)); err != nil {
				t.Fatalf("write compressed csv: %v", err)
			}

			rows, closeFn, err := ParseCSVFromZST(context.Background(), path)
			if err != nil {
				t.Fatalf("ParseCSVFromZST: %v", err)
			}
			defer closeFn()

			var got []TickRow
			for row := range rows {
				got = append(got, row)
			}

			if len(got) != 1 {
				t.Fatalf("row count = %d, want 1", len(got))
			}
			if got[0].Symbol != "BTC-3FEB24-42250-P" {
				t.Fatalf("symbol = %q, want %q", got[0].Symbol, "BTC-3FEB24-42250-P")
			}
			if !got[0].Timestamp.Equal(expectedTime) {
				t.Fatalf("timestamp = %s, want %s", got[0].Timestamp, expectedTime)
			}
		})
	}
}
