package cryptooptions

import (
	"fmt"
	"os"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

// WriteParquet writes a slice of Bar1m records to a Parquet file with
// ZSTD compression.
func WriteParquet(path string, bars []Bar1m) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create parquet file %s: %w", path, err)
	}
	defer f.Close()

	writer := parquet.NewGenericWriter[Bar1m](f,
		parquet.Compression(&zstd.Codec{}),
		parquet.CreatedBy("toktik", "1.0", ""),
	)

	const rowGroupSize = 100_000
	for i := 0; i < len(bars); i += rowGroupSize {
		end := i + rowGroupSize
		if end > len(bars) {
			end = len(bars)
		}
		_, err := writer.Write(bars[i:end])
		if err != nil {
			writer.Close()
			return fmt.Errorf("write rows to %s: %w", path, err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close parquet writer %s: %w", path, err)
	}

	return nil
}

// ReadParquet opens a Parquet file and returns a channel of Bar1m records
// and a closer function.
func ReadParquet(path string) (<-chan Bar1m, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open parquet file %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("stat parquet file %s: %w", path, err)
	}
	_ = info

	reader := parquet.NewGenericReader[Bar1m](f)

	ch := make(chan Bar1m, 4096)
	closeFn := func() {
		reader.Close()
		f.Close()
	}

	go func() {
		defer close(ch)
		buf := make([]Bar1m, 1024)
		for {
			n, err := reader.Read(buf)
			for i := 0; i < n; i++ {
				ch <- buf[i]
			}
			if err != nil {
				break
			}
		}
	}()

	return ch, closeFn, nil
}
