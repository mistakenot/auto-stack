package commands

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/datadyne-io/autodoc/internal/search"
)

// SearchKeyword performs a BM25 keyword search and writes JSON results to w.
// indexPath is the absolute path to the Bluge index directory.
// Returns an error if the index doesn't exist.
func SearchKeyword(w io.Writer, indexPath string, query string) error {
	if !search.IndexExists(indexPath) {
		return fmt.Errorf("index not found at %q: run 'autodoc search reindex' to build the index", indexPath)
	}

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		return fmt.Errorf("opening index: %w", err)
	}
	defer func() { _ = idx.Close() }()

	results, err := idx.Search(query, 10)
	if err != nil {
		return fmt.Errorf("searching index: %w", err)
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling results: %w", err)
	}

	_, err = w.Write(out)
	return err
}
