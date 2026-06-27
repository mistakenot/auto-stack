package source

import (
	"errors"
	"testing"

	"github.com/mistakenot/auto-skill/internal/transport"
)

// mockResolver is a test RefResolver that resolves a fixed set of refs.
type mockResolver struct {
	refs map[string]bool
}

func (m *mockResolver) ResolveRef(ref string) bool {
	return m.refs[ref]
}

func TestParseSourceNormalization(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		opts         ParseOptions
		wantHost     string
		wantRepoPath []string
		wantURL      string
		wantRef      string
		wantSubpath  string
		wantLocal    bool
	}{
		{
			name:         "bare owner/repo",
			input:        "mistakenot/skills",
			wantHost:     "github.com",
			wantRepoPath: []string{"mistakenot", "skills"},
			wantURL:      "https://github.com/mistakenot/skills",
		},
		{
			name:         "host/path",
			input:        "github.com/mistakenot/skills",
			wantHost:     "github.com",
			wantRepoPath: []string{"mistakenot", "skills"},
			wantURL:      "https://github.com/mistakenot/skills",
		},
		{
			name:         "https URL",
			input:        "https://github.com/mistakenot/skills",
			wantHost:     "github.com",
			wantRepoPath: []string{"mistakenot", "skills"},
			wantURL:      "https://github.com/mistakenot/skills",
		},
		{
			name:         "https with .git",
			input:        "https://github.com/mistakenot/skills.git",
			wantHost:     "github.com",
			wantRepoPath: []string{"mistakenot", "skills"},
			wantURL:      "https://github.com/mistakenot/skills",
		},
		{
			name:         "https with trailing slash",
			input:        "https://github.com/mistakenot/skills/",
			wantHost:     "github.com",
			wantRepoPath: []string{"mistakenot", "skills"},
			wantURL:      "https://github.com/mistakenot/skills",
		},
		{
			name:         "ssh shorthand",
			input:        "git@github.com:mistakenot/skills.git",
			wantHost:     "github.com",
			wantRepoPath: []string{"mistakenot", "skills"},
			wantURL:      "https://github.com/mistakenot/skills",
		},
		{
			name:         "ssh URL",
			input:        "ssh://git@github.com/mistakenot/skills",
			wantHost:     "github.com",
			wantRepoPath: []string{"mistakenot", "skills"},
			wantURL:      "https://github.com/mistakenot/skills",
		},
		{
			name:         "nested gitlab groups",
			input:        "gitlab.com/acme/platform/skills",
			wantHost:     "gitlab.com",
			wantRepoPath: []string{"acme", "platform", "skills"},
			wantURL:      "https://gitlab.com/acme/platform/skills",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := ParseSource(tt.input, tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src.Local != tt.wantLocal {
				t.Errorf("Local = %v, want %v", src.Local, tt.wantLocal)
			}
			if src.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", src.Host, tt.wantHost)
			}
			if len(src.RepoPath) != len(tt.wantRepoPath) {
				t.Fatalf("RepoPath = %v, want %v", src.RepoPath, tt.wantRepoPath)
			}
			for i, p := range src.RepoPath {
				if p != tt.wantRepoPath[i] {
					t.Errorf("RepoPath[%d] = %q, want %q", i, p, tt.wantRepoPath[i])
				}
			}
			if src.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", src.URL, tt.wantURL)
			}
			if src.Ref != tt.wantRef {
				t.Errorf("Ref = %q, want %q", src.Ref, tt.wantRef)
			}
			if src.Subpath != tt.wantSubpath {
				t.Errorf("Subpath = %q, want %q", src.Subpath, tt.wantSubpath)
			}
		})
	}
}

func TestParseSourceCredentialRejection(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"userinfo in URL", "https://user:token@github.com/o/r"},
		{"access_token query", "https://github.com/o/r?access_token=xxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource(tt.input, ParseOptions{})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var te *transport.TransportError
			if !errors.As(err, &te) {
				t.Fatalf("expected *TransportError, got %T: %v", err, err)
			}
			if te.Code != transport.CodeCredentialsInURL {
				t.Errorf("error code = %q, want %q", te.Code, transport.CodeCredentialsInURL)
			}
		})
	}
}

func TestParseSourceTransportRejection(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"remote helper ext::", "ext::ssh foo"},
		{"leading dash flag", "-flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource(tt.input, ParseOptions{})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var te *transport.TransportError
			if !errors.As(err, &te) {
				t.Fatalf("expected *TransportError, got %T: %v", err, err)
			}
			if te.Code != transport.CodeUnsupportedTransport {
				t.Errorf("error code = %q, want %q", te.Code, transport.CodeUnsupportedTransport)
			}
		})
	}
}

func TestParseSourceDeepLinks(t *testing.T) {
	resolver := &mockResolver{refs: map[string]bool{
		"main":      true,
		"feature/x": true,
	}}

	tests := []struct {
		name        string
		input       string
		opts        ParseOptions
		wantHost    string
		wantURL     string
		wantRef     string
		wantSubpath string
	}{
		{
			name:  "github deep-link main",
			input: "https://github.com/owner/repo/tree/main/skills/foo",
			opts:  ParseOptions{RefResolver: resolver},

			wantHost:    "github.com",
			wantURL:     "https://github.com/owner/repo",
			wantRef:     "main",
			wantSubpath: "skills/foo",
		},
		{
			name:  "github deep-link slashy ref",
			input: "https://github.com/owner/repo/tree/feature/x/skills/foo",
			opts:  ParseOptions{RefResolver: resolver},

			wantHost:    "github.com",
			wantURL:     "https://github.com/owner/repo",
			wantRef:     "feature/x",
			wantSubpath: "skills/foo",
		},
		{
			name:  "gitlab deep-link",
			input: "https://gitlab.com/group/repo/-/tree/main/skills/foo",
			opts:  ParseOptions{RefResolver: resolver},

			wantHost:    "gitlab.com",
			wantURL:     "https://gitlab.com/group/repo",
			wantRef:     "main",
			wantSubpath: "skills/foo",
		},
		{
			name:    "version override replaces deep-link ref",
			input:   "https://github.com/owner/repo/tree/main/skills/foo",
			opts:    ParseOptions{RefResolver: resolver, Version: "v2.0"},
			wantRef: "v2.0",

			wantHost:    "github.com",
			wantURL:     "https://github.com/owner/repo",
			wantSubpath: "skills/foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := ParseSource(tt.input, tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", src.Host, tt.wantHost)
			}
			if src.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", src.URL, tt.wantURL)
			}
			if src.Ref != tt.wantRef {
				t.Errorf("Ref = %q, want %q", src.Ref, tt.wantRef)
			}
			if src.Subpath != tt.wantSubpath {
				t.Errorf("Subpath = %q, want %q", src.Subpath, tt.wantSubpath)
			}
		})
	}
}

func TestParseSourceDeepLinkNoResolve(t *testing.T) {
	resolver := &mockResolver{refs: map[string]bool{}} // nothing resolves

	_, err := ParseSource("https://github.com/owner/repo/tree/mystery/branch/path", ParseOptions{
		RefResolver: resolver,
	})
	if err == nil {
		t.Fatal("expected DeepLinkError, got nil")
	}
	var dle *DeepLinkError
	if !errors.As(err, &dle) {
		t.Fatalf("expected *DeepLinkError, got %T: %v", err, err)
	}
}

func TestParseSourceLocalPaths(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantURL string
	}{
		{"relative dot-slash", "./local/path", "./local/path"},
		{"absolute path", "/absolute/path", "/absolute/path"},
		{"relative dot-dot", "../relative", "../relative"},
		{"file URL", "file:///some/path", "/some/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := ParseSource(tt.input, ParseOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !src.Local {
				t.Error("Local = false, want true")
			}
			if src.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", src.URL, tt.wantURL)
			}
		})
	}
}

func TestParseSourceEmpty(t *testing.T) {
	_, err := ParseSource("", ParseOptions{})
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
	var te *transport.TransportError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TransportError, got %T: %v", err, err)
	}
	if te.Code != transport.CodeUnsupportedTransport {
		t.Errorf("error code = %q, want %q", te.Code, transport.CodeUnsupportedTransport)
	}
}

func TestParseSourceVersionWithoutDeepLink(t *testing.T) {
	src, err := ParseSource("mistakenot/skills", ParseOptions{Version: "v1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Ref != "v1.0" {
		t.Errorf("Ref = %q, want %q", src.Ref, "v1.0")
	}
}

func TestSplitDeepLink(t *testing.T) {
	tests := []struct {
		name        string
		segs        []string
		refs        map[string]bool
		wantRef     string
		wantSubpath string
		wantErr     bool
	}{
		{
			name: "simple ref",
			segs: []string{"main", "skills", "foo"},
			refs: map[string]bool{"main": true},

			wantRef:     "main",
			wantSubpath: "skills/foo",
		},
		{
			name: "slashy ref",
			segs: []string{"feature", "x", "skills", "foo"},
			refs: map[string]bool{"feature/x": true},

			wantRef:     "feature/x",
			wantSubpath: "skills/foo",
		},
		{
			name: "ref is entire path",
			segs: []string{"v1.0"},
			refs: map[string]bool{"v1.0": true},

			wantRef:     "v1.0",
			wantSubpath: "",
		},
		{
			name:    "nothing resolves",
			segs:    []string{"unknown", "branch"},
			refs:    map[string]bool{},
			wantErr: true,
		},
		{
			name:    "empty segs",
			segs:    []string{},
			refs:    map[string]bool{"main": true},
			wantErr: true,
		},
		{
			name: "longest match wins",
			segs: []string{"feature", "x", "skills"},
			refs: map[string]bool{
				"feature":          true,
				"feature/x":        true,
				"feature/x/skills": true,
			},
			wantRef:     "feature/x/skills",
			wantSubpath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockResolver{refs: tt.refs}
			ref, subpath, err := SplitDeepLink(tt.segs, resolver)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var dle *DeepLinkError
				if !errors.As(err, &dle) {
					t.Fatalf("expected *DeepLinkError, got %T: %v", err, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref != tt.wantRef {
				t.Errorf("ref = %q, want %q", ref, tt.wantRef)
			}
			if subpath != tt.wantSubpath {
				t.Errorf("subpath = %q, want %q", subpath, tt.wantSubpath)
			}
		})
	}
}

func TestParseSourceAllFormsConverge(t *testing.T) {
	// All surface forms for the same repo must produce the same Host+RepoPath+URL.
	forms := []string{
		"mistakenot/skills",
		"github.com/mistakenot/skills",
		"https://github.com/mistakenot/skills",
		"https://github.com/mistakenot/skills.git",
		"https://github.com/mistakenot/skills/",
		"git@github.com:mistakenot/skills.git",
		"ssh://git@github.com/mistakenot/skills",
	}

	var first Source
	for i, form := range forms {
		src, err := ParseSource(form, ParseOptions{})
		if err != nil {
			t.Fatalf("ParseSource(%q) error: %v", form, err)
		}
		if i == 0 {
			first = src
			continue
		}
		if src.Host != first.Host {
			t.Errorf("form %q Host = %q, want %q", form, src.Host, first.Host)
		}
		if src.URL != first.URL {
			t.Errorf("form %q URL = %q, want %q", form, src.URL, first.URL)
		}
		if len(src.RepoPath) != len(first.RepoPath) {
			t.Errorf("form %q RepoPath = %v, want %v", form, src.RepoPath, first.RepoPath)
			continue
		}
		for j, p := range src.RepoPath {
			if p != first.RepoPath[j] {
				t.Errorf("form %q RepoPath[%d] = %q, want %q", form, j, p, first.RepoPath[j])
			}
		}
	}
}
