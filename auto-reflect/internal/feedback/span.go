package feedback

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-reflect/internal/gitutil"
)

type SpanCapture struct {
	StartLine       int
	EndLine         int
	StartByte       int
	EndByte         int
	ContentSnippet  string
	SnippetSHA256   string
	HeadBlobSHA     *string
	ObservedBlobSHA string
	CaptureSource   string
	WorktreeDirty   bool
}

type lineBounds struct {
	start int
	end   int
}

func CaptureSpan(repoRoot, repoRelativeFile string, startLine, endLine int, repoDirty bool) (SpanCapture, error) {
	abs := filepath.Join(repoRoot, filepath.FromSlash(repoRelativeFile))
	bytes, err := os.ReadFile(abs)
	if err != nil {
		return SpanCapture{}, fmt.Errorf("read file for span capture: %w", err)
	}

	lines := computeLineBounds(bytes)
	if startLine < 1 {
		return SpanCapture{}, errors.New("invalid span: start_line must be >= 1; use --start <n> where n >= 1")
	}
	if endLine < startLine {
		return SpanCapture{}, errors.New("invalid span: end_line must be >= start_line; adjust --end")
	}
	if endLine > len(lines) {
		return SpanCapture{}, fmt.Errorf("invalid span: end_line exceeds file length (%d lines); adjust --end", len(lines))
	}

	startByte := lines[startLine-1].start
	endByte := lines[endLine-1].end
	snippetBytes := bytes[startByte:endByte]
	snippet := string(snippetBytes)

	snippetHash := sha256.Sum256(snippetBytes)
	snippetSHA := hex.EncodeToString(snippetHash[:])

	observedBlobSHA := gitutil.ComputeGitBlobSHA(bytes)
	fileDirty, err := gitutil.FileDirty(repoRoot, repoRelativeFile)
	if err != nil {
		return SpanCapture{}, err
	}

	var headBlob *string
	headBlobValue, headErr := gitutil.HeadBlobSHA(repoRoot, repoRelativeFile)
	if headErr == nil && headBlobValue != "" {
		headBlob = &headBlobValue
	}

	captureSource := "working_tree"
	if headBlob != nil && !fileDirty {
		captureSource = "head"
	}

	return SpanCapture{
		StartLine:       startLine,
		EndLine:         endLine,
		StartByte:       startByte,
		EndByte:         endByte,
		ContentSnippet:  snippet,
		SnippetSHA256:   snippetSHA,
		HeadBlobSHA:     headBlob,
		ObservedBlobSHA: observedBlobSHA,
		CaptureSource:   captureSource,
		WorktreeDirty:   repoDirty,
	}, nil
}

func computeLineBounds(data []byte) []lineBounds {
	if len(data) == 0 {
		return []lineBounds{}
	}

	bounds := make([]lineBounds, 0, 64)
	start := 0
	for i, b := range data {
		if b == '\n' {
			bounds = append(bounds, lineBounds{start: start, end: i + 1})
			start = i + 1
		}
	}
	if start < len(data) {
		bounds = append(bounds, lineBounds{start: start, end: len(data)})
	}
	return bounds
}
