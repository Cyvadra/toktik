package csvutil

import (
	"bufio"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// stackedReadCloser reads from reader and closes all closers in order.
type stackedReadCloser struct {
	reader  io.Reader
	closers []io.Closer
}

func (s *stackedReadCloser) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *stackedReadCloser) Close() error {
	var firstErr error
	for _, closer := range s.closers {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// OpenMaybeGzipCSV opens a CSV file and auto-wraps gzip files by ".gz" suffix.
// A bufferSize of zero or negative uses a default buffer size of 4 MiB.
func OpenMaybeGzipCSV(path string, bufferSize int) (io.ReadCloser, *csv.Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}

	var reader io.ReadCloser = f
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("gzip reader %s: %w", path, err)
		}
		reader = &stackedReadCloser{reader: gz, closers: []io.Closer{gz, f}}
	}

	if bufferSize <= 0 {
		bufferSize = 4 * 1024 * 1024
	}
	csvReader := csv.NewReader(bufio.NewReaderSize(reader, bufferSize))
	csvReader.ReuseRecord = true
	return reader, csvReader, nil
}
