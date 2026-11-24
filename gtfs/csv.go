// Package gtfs reads the subset of a GTFS feed needed to draw a bus network.
package gtfs

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Row is one CSV record addressed by column name, so column order in the
// file does not matter (stop_times.txt in the TGSRTC feed is non-standard).
type Row struct {
	cols map[string]int
	rec  []string
}

// Get returns the trimmed value of the named column, or "" if the column is
// missing or the record is short.
func (r Row) Get(name string) string {
	i, ok := r.cols[name]
	if !ok || i >= len(r.rec) {
		return ""
	}
	return strings.TrimSpace(r.rec[i])
}

// ReadCSV streams records from r, calling fn for each one. The Row is only
// valid during the call. required lists columns that must be in the header.
func ReadCSV(r io.Reader, required []string, fn func(Row) error) error {
	cr := csv.NewReader(bufio.NewReaderSize(r, 1<<20))
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	header, err := cr.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty file: no header row")
		}
		return fmt.Errorf("reading header: %w", err)
	}
	cols := make(map[string]int, len(header))
	for i, h := range header {
		h = strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))
		if _, dup := cols[h]; !dup {
			cols[h] = i
		}
	}
	for _, name := range required {
		if _, ok := cols[name]; !ok {
			return fmt.Errorf("missing required column %q", name)
		}
	}

	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(Row{cols: cols, rec: rec}); err != nil {
			return err
		}
	}
}

// readFile opens path and streams it through ReadCSV, adding the file name to errors.
func readFile(path string, required []string, fn func(Row) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read-only: a close error is not actionable
	if err := ReadCSV(f, required, fn); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
