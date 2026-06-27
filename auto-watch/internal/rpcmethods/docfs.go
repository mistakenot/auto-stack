package rpcmethods

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"golang.org/x/net/html"
)

// docEntry is a single doc file returned by doc.list.
type docEntry struct {
	ID   string    `json:"id"`
	Path string    `json:"path"`
	Type string    `json:"type"`
	Meta *PlanMeta `json:"meta,omitempty"`
}

// projectEntry is a single project returned by project.list.
type projectEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote"`
	Host   string `json:"host"`
}

// PlanMeta holds lifecycle metadata extracted from an HTML planning document.
type PlanMeta struct {
	Status      string `json:"status,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Epic        string `json:"epic,omitempty"`
	PR          string `json:"pr,omitempty"`
	Created     string `json:"created,omitempty"`
	ReviewState string `json:"reviewState,omitempty"`
}

// MaxMetaPrefixBytes is the maximum number of bytes read from an HTML file
// when extracting pd-meta.
const MaxMetaPrefixBytes = 8192

// resolveRoot determines the filesystem root for doc operations. It validates
// the worktree (if given) against the project registry — an arbitrary
// client-supplied path is never accepted as the read root.
func resolveRoot(reg config.ProjectsConfig, project, worktree string) (string, error) {
	if worktree != "" {
		if ref := reg.FindProjectByPath(worktree); ref != nil {
			return filepath.Clean(worktree), nil
		}
		return "", errors.New("worktree not found in registry")
	}
	if project != "" {
		if ref := reg.FindProjectByID(project); ref != nil {
			return filepath.Clean(ref.Path), nil
		}
		return "", errors.New("project not found in registry")
	}
	return "", errors.New("project or worktree is required")
}

// cleanDocPath validates and cleans a path for doc operations. It returns "" if
// the path is invalid (traversal, outside docs/, or extension not in allowed).
func cleanDocPath(p string, allowed ...string) string {
	cleaned := path.Clean(p)

	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/..") {
		return ""
	}

	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, "/")

	if !strings.HasPrefix(cleaned, "docs/") {
		return ""
	}

	ext := false
	for _, a := range allowed {
		if strings.HasSuffix(cleaned, a) {
			ext = true
			break
		}
	}
	if !ext {
		return ""
	}

	return cleaned
}

// walkDocs walks the docs/ directory under root and returns entries for all
// *.md and *.html files found there (at any depth).
func walkDocs(root string) ([]docEntry, error) {
	docsDir := filepath.Join(root, "docs")
	entries := []docEntry{}

	err := filepath.WalkDir(docsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		var docType string
		switch {
		case strings.HasSuffix(d.Name(), ".md"):
			docType = "markdown"
		case strings.HasSuffix(d.Name(), ".html"):
			docType = "html"
		default:
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil //nolint:nilerr
		}
		rel = filepath.ToSlash(rel)

		var meta *PlanMeta
		if docType == "html" {
			if f, err := os.Open(p); err == nil {
				meta = ExtractPlanMeta(io.LimitReader(f, MaxMetaPrefixBytes))
				_ = f.Close()
			}
		}

		entries = append(entries, docEntry{
			ID:   rel,
			Path: rel,
			Type: docType,
			Meta: meta,
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, err
	}
	return entries, nil
}

// ExtractPlanMeta parses the bounded prefix of an HTML planning document and
// returns lifecycle metadata. Returns nil if neither signal is found.
func ExtractPlanMeta(r io.Reader) *PlanMeta {
	tokenizer := html.NewTokenizer(r)

	var meta *PlanMeta
	inPdMeta := false

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return meta

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tagName := string(tn)

			if tagName == "script" && hasAttr {
				id, typ := "", ""
				for {
					key, val, more := tokenizer.TagAttr()
					k := string(key)
					if k == "id" {
						id = string(val)
					} else if k == "type" {
						typ = string(val)
					}
					if !more {
						break
					}
				}
				if id == "pd-meta" && typ == "application/json" {
					inPdMeta = true
				}
			}

			if tagName == "pd-doc" && hasAttr {
				for {
					key, val, more := tokenizer.TagAttr()
					if string(key) == "status" {
						if meta == nil {
							meta = &PlanMeta{}
						}
						meta.ReviewState = string(val)
					}
					if !more {
						break
					}
				}
			}

		case html.TextToken:
			if inPdMeta {
				inPdMeta = false
				text := strings.TrimSpace(string(tokenizer.Text()))
				if text == "" {
					continue
				}
				var raw struct {
					Status  string  `json:"status"`
					Branch  *string `json:"branch"`
					Epic    string  `json:"epic"`
					PR      *string `json:"pr"`
					Created string  `json:"created"`
				}
				if err := json.Unmarshal([]byte(text), &raw); err != nil {
					continue
				}
				if meta == nil {
					meta = &PlanMeta{}
				}
				meta.Status = raw.Status
				meta.Epic = raw.Epic
				meta.Created = raw.Created
				if raw.Branch != nil {
					meta.Branch = *raw.Branch
				}
				if raw.PR != nil {
					meta.PR = *raw.PR
				}
			}

		case html.EndTagToken:
			if inPdMeta {
				inPdMeta = false
			}
		}
	}
}
