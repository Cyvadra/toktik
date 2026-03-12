package cryptooptions

import (
	"fmt"
	"os"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

const parquetRowGroupSize = 100_000

type BarWriter struct {
	file   *os.File
	writer *parquet.GenericWriter[Bar1m]
}

func NewBarWriter(path string) (*BarWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create parquet file %s: %w", path, err)
	}

	writer := parquet.NewGenericWriter[Bar1m](f,
		parquet.Compression(&zstd.Codec{}),
		parquet.CreatedBy("toktik", "1.0", ""),
	)

	return &BarWriter{file: f, writer: writer}, nil
}

func (w *BarWriter) WriteRows(rows []Bar1m) error {
	for i := 0; i < len(rows); i += parquetRowGroupSize {
		end := i + parquetRowGroupSize
		if end > len(rows) {
			end = len(rows)
		}
		if _, err := w.writer.Write(rows[i:end]); err != nil {
			return fmt.Errorf("write rows: %w", err)
		}
	}
	return nil
}

func (w *BarWriter) Close() error {
	if w == nil {
		return nil
	}
	if w.writer == nil && w.file == nil {
		return nil
	}

	writer := w.writer
	file := w.file
	w.writer = nil
	w.file = nil

	if err := writer.Close(); err != nil {
		if file != nil {
			file.Close()
			return fmt.Errorf("close parquet writer %s: %w", file.Name(), err)
		}
		return fmt.Errorf("close parquet writer: %w", err)
	}
	if file != nil {
		if err := file.Close(); err != nil {
			return fmt.Errorf("close parquet file %s: %w", file.Name(), err)
		}
	}
	return nil
}

// WriteParquet writes a slice of Bar1m records to a Parquet file with
// ZSTD compression.
func WriteParquet(path string, bars []Bar1m) error {
	writer, err := NewBarWriter(path)
	if err != nil {
		return err
	}
	defer writer.Close()

	if err := writer.WriteRows(bars); err != nil {
		return fmt.Errorf("write rows to %s: %w", path, err)
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
