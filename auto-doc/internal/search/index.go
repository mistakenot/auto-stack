// Package search provides BM25 keyword search over documentation files using Bluge.
package search

import (
	"context"
	"os"
	"sort"

	"github.com/blugelabs/bluge"
)

// SearchResult holds a single search result returned from the index.
type SearchResult struct {
	Score   float64 `json:"score"`
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Summary string  `json:"summary"`
	Snippet string  `json:"snippet"`
}

// Index wraps a Bluge writer for document indexing and searching.
type Index struct {
	writer *bluge.Writer
	path   string
}

// OpenIndex opens or creates a Bluge index at the given directory path.
func OpenIndex(indexPath string) (*Index, error) {
	config := bluge.DefaultConfig(indexPath)
	writer, err := bluge.OpenWriter(config)
	if err != nil {
		return nil, err
	}
	return &Index{writer: writer, path: indexPath}, nil
}

// Close closes the index writer.
func (idx *Index) Close() error {
	return idx.writer.Close()
}

// UpsertDoc indexes a single document (upsert by file path as ID).
// It updates an existing document if one with the same relPath already exists.
func (idx *Index) UpsertDoc(relPath, title, summary, headings, body string) error {
	doc := bluge.NewDocument(relPath).
		AddField(bluge.NewKeywordField("path", relPath).StoreValue()).
		AddField(bluge.NewTextField("title", title).StoreValue()).
		AddField(bluge.NewTextField("summary", summary).StoreValue()).
		AddField(bluge.NewTextField("headings", headings).StoreValue()).
		AddField(bluge.NewTextField("body", body).StoreValue().HighlightMatches()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id", "path"}))

	return idx.writer.Update(doc.ID(), doc)
}

// DeleteDoc removes a document from the index by its file path.
func (idx *Index) DeleteDoc(relPath string) error {
	return idx.writer.Delete(bluge.Identifier(relPath))
}

// ListDocPaths returns all indexed document paths.
func (idx *Index) ListDocPaths() ([]string, error) {
	reader, err := idx.writer.Reader()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	count, err := reader.Count()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return []string{}, nil
	}

	req := bluge.NewTopNSearch(int(count), bluge.NewMatchAllQuery())
	iter, err := reader.Search(context.Background(), req)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, count)
	for {
		match, err := iter.Next()
		if err != nil {
			return nil, err
		}
		if match == nil {
			break
		}

		path := ""
		err = match.VisitStoredFields(func(field string, value []byte) bool {
			if field == "path" {
				path = string(value)
				return false
			}
			return true
		})
		if err != nil {
			return nil, err
		}
		if path != "" {
			paths = append(paths, path)
		}
	}

	sort.Strings(paths)
	return paths, nil
}

// Search performs a BM25 keyword search and returns top N results.
// Results are sorted by score descending. Returns an empty (non-nil) slice when no results match.
func (idx *Index) Search(query string, limit int) ([]SearchResult, error) {
	results := []SearchResult{}

	reader, err := idx.writer.Reader()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	q := bluge.NewMatchQuery(query).SetField("_all")
	request := bluge.NewTopNSearch(limit, q).
		WithStandardAggregations()

	iter, err := reader.Search(context.Background(), request)
	if err != nil {
		return nil, err
	}

	for {
		match, err := iter.Next()
		if err != nil {
			return nil, err
		}
		if match == nil {
			break
		}

		var path, title, summary, body string

		err = match.VisitStoredFields(func(field string, value []byte) bool {
			switch field {
			case "path":
				path = string(value)
			case "title":
				title = string(value)
			case "summary":
				summary = string(value)
			case "body":
				body = string(value)
			}
			return true
		})
		if err != nil {
			return nil, err
		}

		snippet := body
		if len(snippet) > 150 {
			snippet = snippet[:150]
		}

		results = append(results, SearchResult{
			Score:   match.Score,
			Path:    path,
			Title:   title,
			Summary: summary,
			Snippet: snippet,
		})
	}

	return results, nil
}

// IndexExists checks if an index directory exists at the given path.
func IndexExists(indexPath string) bool {
	_, err := os.Stat(indexPath)
	return !os.IsNotExist(err)
}
