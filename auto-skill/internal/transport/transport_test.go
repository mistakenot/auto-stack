package transport

import (
	"errors"
	"testing"
)

func TestContainsCredentials(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"clean https", "https://github.com/acme/repo", false},
		{"clean ssh", "ssh://git@github.com/acme/repo", false},
		{"ssh shorthand", "git@github.com:acme/repo", false},
		{"userinfo", "https://user:token@github.com/acme/repo", true},
		{"user only", "https://user@github.com/acme/repo", true},
		{"access_token query", "https://github.com/acme/repo?access_token=xxx", true},
		{"private_token query", "https://github.com/acme/repo?private_token=xxx", true},
		{"token query", "https://github.com/acme/repo?token=xxx", true},
		{"x-access-token query", "https://github.com/acme/repo?x-access-token=xxx", true},
		{"empty", "", false},
		{"bare host", "github.com/acme/repo", false},
		{"file url", "file:///tmp/repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsCredentials(tt.url)
			if got != tt.want {
				t.Errorf("ContainsCredentials(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCanon  string
		wantHost   string
		wantPath   []string
		wantErrMsg string
		wantCode   string
	}{
		{
			name:      "https basic",
			input:     "https://github.com/acme/skills",
			wantCanon: "https://github.com/acme/skills",
			wantHost:  "github.com",
			wantPath:  []string{"acme", "skills"},
		},
		{
			name:      "https with .git",
			input:     "https://github.com/acme/skills.git",
			wantCanon: "https://github.com/acme/skills",
			wantHost:  "github.com",
			wantPath:  []string{"acme", "skills"},
		},
		{
			name:      "ssh with git@ prefix",
			input:     "ssh://git@github.com/acme/skills",
			wantCanon: "https://github.com/acme/skills",
			wantHost:  "github.com",
			wantPath:  []string{"acme", "skills"},
		},
		{
			name:      "ssh shorthand",
			input:     "git@github.com:acme/skills.git",
			wantCanon: "https://github.com/acme/skills",
			wantHost:  "github.com",
			wantPath:  []string{"acme", "skills"},
		},
		{
			name:      "git protocol",
			input:     "git://github.com/acme/skills.git",
			wantCanon: "https://github.com/acme/skills",
			wantHost:  "github.com",
			wantPath:  []string{"acme", "skills"},
		},
		{
			name:      "bare host/path",
			input:     "github.com/acme/skills",
			wantCanon: "https://github.com/acme/skills",
			wantHost:  "github.com",
			wantPath:  []string{"acme", "skills"},
		},
		{
			name:      "nested gitlab groups",
			input:     "https://gitlab.com/acme/platform/skills",
			wantCanon: "https://gitlab.com/acme/platform/skills",
			wantHost:  "gitlab.com",
			wantPath:  []string{"acme", "platform", "skills"},
		},
		{
			name:      "uppercase host",
			input:     "https://GitHub.COM/Acme/Skills",
			wantCanon: "https://github.com/Acme/Skills",
			wantHost:  "github.com",
			wantPath:  []string{"Acme", "Skills"},
		},
		{
			name:      "strip default port",
			input:     "https://github.com:443/acme/skills",
			wantCanon: "https://github.com/acme/skills",
			wantHost:  "github.com",
			wantPath:  []string{"acme", "skills"},
		},
		{
			name:      "file URL",
			input:     "file:///tmp/my-repo",
			wantCanon: "file:///tmp/my-repo",
			wantHost:  "_local",
			wantPath:  []string{"tmp", "my-repo"},
		},
		// Rejection cases
		{
			name:     "reject ext:: helper",
			input:    "ext::ssh -o ProxyCommand=evil %S repo.git",
			wantCode: CodeUnsupportedTransport,
		},
		{
			name:     "reject fd:: helper",
			input:    "fd::17",
			wantCode: CodeUnsupportedTransport,
		},
		{
			name:     "reject ftp",
			input:    "ftp://host/repo",
			wantCode: CodeUnsupportedTransport,
		},
		{
			name:     "reject leading dash",
			input:    "-c http.proxy=evil",
			wantCode: CodeUnsupportedTransport,
		},
		{
			name:     "reject credentials userinfo",
			input:    "https://user:token@github.com/acme/repo",
			wantCode: CodeCredentialsInURL,
		},
		{
			name:     "reject credentials access_token",
			input:    "https://github.com/acme/repo?access_token=xxx",
			wantCode: CodeCredentialsInURL,
		},
		{
			name:     "reject credentials private_token",
			input:    "https://github.com/acme/repo?private_token=xxx",
			wantCode: CodeCredentialsInURL,
		},
		{
			name:     "reject empty",
			input:    "",
			wantCode: CodeUnsupportedTransport,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canon, id, err := CanonicalizeURL(tt.input)
			if tt.wantCode != "" {
				if err == nil {
					t.Fatalf("expected error with code %q, got nil", tt.wantCode)
				}
				var te *TransportError
				if !errors.As(err, &te) {
					t.Fatalf("expected *TransportError, got %T: %v", err, err)
				}
				if te.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q (msg: %s)", te.Code, tt.wantCode, te.Message)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if canon != tt.wantCanon {
				t.Errorf("canonical = %q, want %q", canon, tt.wantCanon)
			}
			if id.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", id.Host, tt.wantHost)
			}
			if len(id.Path) != len(tt.wantPath) {
				t.Fatalf("path = %v, want %v", id.Path, tt.wantPath)
			}
			for i, p := range id.Path {
				if p != tt.wantPath[i] {
					t.Errorf("path[%d] = %q, want %q", i, p, tt.wantPath[i])
				}
			}
		})
	}
}

func TestCanonicalizeURLIdentityStability(t *testing.T) {
	// HTTPS and SSH forms of the same repo must yield the same identity.
	forms := []string{
		"https://github.com/acme/skills",
		"https://github.com/acme/skills.git",
		"ssh://git@github.com/acme/skills",
		"git@github.com:acme/skills.git",
		"git://github.com/acme/skills.git",
		"github.com/acme/skills",
	}

	var firstCanon string
	var firstID CacheIdentity
	for i, form := range forms {
		canon, id, err := CanonicalizeURL(form)
		if err != nil {
			t.Fatalf("CanonicalizeURL(%q) error: %v", form, err)
		}
		if i == 0 {
			firstCanon = canon
			firstID = id
			continue
		}
		if canon != firstCanon {
			t.Errorf("form %q canonical = %q, want %q", form, canon, firstCanon)
		}
		if id.Host != firstID.Host {
			t.Errorf("form %q host = %q, want %q", form, id.Host, firstID.Host)
		}
		if id.RelPath() != firstID.RelPath() {
			t.Errorf("form %q relpath = %q, want %q", form, id.RelPath(), firstID.RelPath())
		}
	}
}

func TestEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantCode string
	}{
		{
			name:  "https default port",
			input: "https://github.com",
			want:  "https://github.com:443",
		},
		{
			name:  "https explicit default port",
			input: "https://github.com:443",
			want:  "https://github.com:443",
		},
		{
			name:  "https non-default port",
			input: "https://github.com:8443",
			want:  "https://github.com:8443",
		},
		{
			name:  "ssh default port",
			input: "ssh://github.com",
			want:  "ssh://github.com:22",
		},
		{
			name:  "git default port",
			input: "git://github.com",
			want:  "git://github.com:9418",
		},
		{
			name:  "uppercase host normalized",
			input: "https://GitHub.COM",
			want:  "https://github.com:443",
		},
		{
			name:  "local absolute path",
			input: "/home/user/repos/skills",
			want:  "/home/user/repos/skills",
		},
		{
			name:  "https with path stripped",
			input: "https://github.com/acme/skills",
			want:  "https://github.com:443",
		},
		{
			name:     "reject ftp",
			input:    "ftp://host/repo",
			wantCode: CodeUnsupportedTransport,
		},
		{
			name:     "reject credentials",
			input:    "https://user:pass@github.com",
			wantCode: CodeCredentialsInURL,
		},
		{
			name:     "reject empty",
			input:    "",
			wantCode: CodeUnsupportedTransport,
		},
		{
			name:     "reject leading dash",
			input:    "-evil",
			wantCode: CodeUnsupportedTransport,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Endpoint(tt.input)
			if tt.wantCode != "" {
				if err == nil {
					t.Fatalf("expected error with code %q, got nil", tt.wantCode)
				}
				var te *TransportError
				if !errors.As(err, &te) {
					t.Fatalf("expected *TransportError, got %T: %v", err, err)
				}
				if te.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q (msg: %s)", te.Code, tt.wantCode, te.Message)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Endpoint(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEndpointPortEquivalence(t *testing.T) {
	a, err := Endpoint("https://github.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Endpoint("https://github.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("https://github.com (%q) != https://github.com:443 (%q)", a, b)
	}
}

func TestCacheIdentityRelPath(t *testing.T) {
	id := CacheIdentity{Host: "github.com", Path: []string{"acme", "skills"}}
	want := "github.com/acme/skills"
	if got := id.RelPath(); got != want {
		t.Errorf("RelPath() = %q, want %q", got, want)
	}
}

func TestCacheIdentityHashSuffix(t *testing.T) {
	id := CacheIdentity{Host: "github.com", Path: []string{"acme", "skills"}}
	suffix := id.HashSuffix("https://github.com/acme/skills")
	if len(suffix) != 8 {
		t.Errorf("HashSuffix length = %d, want 8", len(suffix))
	}
}
