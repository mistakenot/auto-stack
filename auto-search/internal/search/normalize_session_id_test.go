package search

import (
	"strings"
	"testing"
)

func TestNormalizeSessionID(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty is noop", in: "", want: ""},
		{name: "whitespace-only is noop", in: "   ", want: ""},
		{name: "valid uuid", in: "ab2a6291-d5fb-4aa3-a590-fc3584911d44", want: "ab2a6291-d5fb-4aa3-a590-fc3584911d44"},
		{name: "valid acompact prefix", in: "acompact-2026-01-15-abc123", want: "acompact-2026-01-15-abc123"},
		{name: "valid subagent prefix", in: "subagent-deadbeef-1234", want: "subagent-deadbeef-1234"},
		{name: "valid short hex", in: "deadbeef", want: "deadbeef"},
		{name: "uppercase is lowered", in: "AB2A6291-D5FB-4AA3-A590-FC3584911D44", want: "ab2a6291-d5fb-4aa3-a590-fc3584911d44"},
		{name: "trim+lower", in: "  AB2A6291-D5FB  ", want: "ab2a6291-d5fb"},
		{name: "rejects too short", in: "xyz", wantErr: true},
		{name: "rejects underscores", in: "abc_def_ghi", wantErr: true},
		{name: "rejects spaces in middle", in: "abc def 123", wantErr: true},
		{name: "rejects path-like input", in: "../../etc/passwd", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeSessionID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil (result %q)", tc.in, got)
				}
				if !strings.Contains(err.Error(), "invalid --session-id value") {
					t.Fatalf("expected error to mention --session-id, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeSessionID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
