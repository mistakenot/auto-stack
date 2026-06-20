package server_test

import (
	"io"
	"strings"
	"testing"

	"github.com/mistakenot/auto-ui/internal/server"
)

func TestExtractPlanMeta_ValidFullBlock(t *testing.T) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <script type="application/json" id="pd-meta">
  {
    "id": "027",
    "name": "plan-status-executor-liveness",
    "status": "executing",
    "branch": "task/027-plan-status-executor-liveness",
    "epic": "002-planning-docs-dashboard",
    "created": "2026-06-18",
    "pr": "https://github.com/example/repo/pull/42"
  }
  </script>
</head>
<body>
  <pd-doc title="027: plan-status" status="approved" pr="pending" generated="2026-06-20">
  </pd-doc>
</body>
</html>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	if meta == nil {
		t.Fatal("expected non-nil PlanMeta")
	}
	if meta.Status != "executing" {
		t.Errorf("Status = %q, want %q", meta.Status, "executing")
	}
	if meta.Branch != "task/027-plan-status-executor-liveness" {
		t.Errorf("Branch = %q, want %q", meta.Branch, "task/027-plan-status-executor-liveness")
	}
	if meta.Epic != "002-planning-docs-dashboard" {
		t.Errorf("Epic = %q, want %q", meta.Epic, "002-planning-docs-dashboard")
	}
	if meta.Created != "2026-06-18" {
		t.Errorf("Created = %q, want %q", meta.Created, "2026-06-18")
	}
	if meta.PR != "https://github.com/example/repo/pull/42" {
		t.Errorf("PR = %q, want %q", meta.PR, "https://github.com/example/repo/pull/42")
	}
	if meta.ReviewState != "approved" {
		t.Errorf("ReviewState = %q, want %q", meta.ReviewState, "approved")
	}
}

func TestExtractPlanMeta_NullBranchAndPR(t *testing.T) {
	html := `<!doctype html>
<head>
  <script type="application/json" id="pd-meta">
  {"status":"planning","branch":null,"epic":"002","created":"2026-06-18","pr":null}
  </script>
</head>
<body>
  <pd-doc status="draft"></pd-doc>
</body>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	if meta == nil {
		t.Fatal("expected non-nil PlanMeta")
	}
	if meta.Status != "planning" {
		t.Errorf("Status = %q, want %q", meta.Status, "planning")
	}
	if meta.Branch != "" {
		t.Errorf("Branch = %q, want empty", meta.Branch)
	}
	if meta.PR != "" {
		t.Errorf("PR = %q, want empty", meta.PR)
	}
	if meta.ReviewState != "draft" {
		t.Errorf("ReviewState = %q, want %q", meta.ReviewState, "draft")
	}
}

func TestExtractPlanMeta_AbsentBlock(t *testing.T) {
	html := `<!doctype html>
<html>
<head><title>Regular Page</title></head>
<body><p>Hello, world!</p></body>
</html>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	if meta != nil {
		t.Errorf("expected nil PlanMeta for plain HTML, got %+v", meta)
	}
}

func TestExtractPlanMeta_MalformedJSON(t *testing.T) {
	html := `<!doctype html>
<head>
  <script type="application/json" id="pd-meta">
  { broken json here!!!
  </script>
</head>
<body>
  <pd-doc status="approved"></pd-doc>
</body>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	// Should still capture ReviewState from pd-doc even though JSON is broken.
	if meta == nil {
		t.Fatal("expected non-nil PlanMeta (ReviewState from pd-doc)")
	}
	if meta.Status != "" {
		t.Errorf("Status = %q, want empty (JSON was malformed)", meta.Status)
	}
	if meta.ReviewState != "approved" {
		t.Errorf("ReviewState = %q, want %q", meta.ReviewState, "approved")
	}
}

func TestExtractPlanMeta_MalformedJSON_NoPdDoc(t *testing.T) {
	html := `<!doctype html>
<head>
  <script type="application/json" id="pd-meta">
  { broken json here!!!
  </script>
</head>
<body><p>No pd-doc element</p></body>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	// No valid JSON, no pd-doc → nil.
	if meta != nil {
		t.Errorf("expected nil PlanMeta, got %+v", meta)
	}
}

func TestExtractPlanMeta_PastBoundedCutoff(t *testing.T) {
	// Place pd-meta well past 8KB so a bounded reader truncates it.
	padding := strings.Repeat("<!-- padding -->", 1000) // ~15KB of padding
	html := `<!doctype html>
<head><title>Test</title></head>
<body>` + padding + `
  <script type="application/json" id="pd-meta">
  {"status":"executing","branch":"task/027","epic":"002","created":"2026-06-18","pr":null}
  </script>
  <pd-doc status="approved"></pd-doc>
</body>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	if meta != nil {
		t.Errorf("expected nil PlanMeta when pd-meta is past 8KB cutoff, got %+v", meta)
	}
}

func TestExtractPlanMeta_MissingPdDoc(t *testing.T) {
	html := `<!doctype html>
<head>
  <script type="application/json" id="pd-meta">
  {"status":"executing","branch":"task/027","epic":"002","created":"2026-06-18","pr":"https://github.com/example/repo/pull/1"}
  </script>
</head>
<body><p>No pd-doc element here</p></body>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	if meta == nil {
		t.Fatal("expected non-nil PlanMeta")
	}
	if meta.Status != "executing" {
		t.Errorf("Status = %q, want %q", meta.Status, "executing")
	}
	if meta.Branch != "task/027" {
		t.Errorf("Branch = %q, want %q", meta.Branch, "task/027")
	}
	if meta.ReviewState != "" {
		t.Errorf("ReviewState = %q, want empty", meta.ReviewState)
	}
}

func TestExtractPlanMeta_NonPlanHTML(t *testing.T) {
	html := `<!doctype html>
<html>
<head>
  <title>Blog Post</title>
  <script type="application/javascript">console.log("hello");</script>
</head>
<body>
  <div class="content">
    <h1>My Blog Post</h1>
    <p>This is a regular HTML page with scripts but no pd-meta.</p>
  </div>
</body>
</html>`

	meta := server.ExtractPlanMeta(io.LimitReader(strings.NewReader(html), server.MaxMetaPrefixBytes))
	if meta != nil {
		t.Errorf("expected nil PlanMeta for non-plan HTML, got %+v", meta)
	}
}
