package etlscan

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mistakenot/auto-search/internal/model"
	"github.com/parquet-go/parquet-go"
)

// ReadMessages reads all message rows from the parquet file at the given path.
func ReadMessages(path string) ([]model.ParquetMessageRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	pf, err := parquet.OpenFile(f, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("open parquet %s: %w", path, err)
	}

	reader := parquet.NewGenericReader[model.ParquetMessageRow](pf)
	defer func() { _ = reader.Close() }()

	var all []model.ParquetMessageRow
	batch := make([]model.ParquetMessageRow, 1024)
	for {
		n, err := reader.Read(batch)
		if n > 0 {
			all = append(all, batch[:n]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read messages from %s: %w", path, err)
		}
	}
	return all, nil
}
