package server

import (
	"encoding/json"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// PlanMeta holds lifecycle metadata extracted from an HTML planning document.
// Fields are populated from the <script id="pd-meta" type="application/json">
// block and the <pd-doc status="..."> element.
type PlanMeta struct {
	Status      string `json:"status,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Epic        string `json:"epic,omitempty"`
	PR          string `json:"pr,omitempty"`
	Created     string `json:"created,omitempty"`
	ReviewState string `json:"reviewState,omitempty"`
}

// MaxMetaPrefixBytes is the maximum number of bytes read from an HTML file
// when extracting pd-meta. The pd-meta block is always in the <head>, so
// 8KB is more than sufficient.
const MaxMetaPrefixBytes = 8192

// ExtractPlanMeta parses the bounded prefix of an HTML planning document and
// returns lifecycle metadata. The caller should wrap the reader with
// io.LimitReader(r, MaxMetaPrefixBytes) before calling.
//
// It extracts two things:
//   - The JSON body of <script id="pd-meta" type="application/json">
//   - The status attribute of the first <pd-doc> start tag
//
// Returns nil if neither signal is found. Tolerant of malformed input.
func ExtractPlanMeta(r io.Reader) *PlanMeta {
	tokenizer := html.NewTokenizer(r)

	var meta *PlanMeta
	inPdMeta := false

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			// EOF or read error — return what we have.
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
				// Parse the pd-meta JSON.
				var raw struct {
					Status  string  `json:"status"`
					Branch  *string `json:"branch"`
					Epic    string  `json:"epic"`
					PR      *string `json:"pr"`
					Created string  `json:"created"`
				}
				if err := json.Unmarshal([]byte(text), &raw); err != nil {
					// Malformed JSON — skip, keep any ReviewState already found.
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
				// Hit </script> before finding text — reset.
				inPdMeta = false
			}
		}
	}
}
