package git

import (
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "https with .git suffix stripped",
			raw:  "https://github.com/owner/repo.git",
			want: "https://github.com/owner/repo",
		},
		{
			name: "https without .git unchanged",
			raw:  "https://github.com/owner/repo",
			want: "https://github.com/owner/repo",
		},
		{
			name: "ssh git@ format converted to https",
			raw:  "git@github.com:owner/repo.git",
			want: "https://github.com/owner/repo",
		},
		{
			name: "ssh:// format converted to https",
			raw:  "ssh://git@host/owner/repo.git",
			want: "https://host/owner/repo",
		},
		{
			name: "uppercase host lowercased",
			raw:  "https://GitHub.COM/owner/repo",
			want: "https://github.com/owner/repo",
		},
		{
			name: "empty input returns empty",
			raw:  "",
			want: "",
		},
		{
			name: "whitespace-only input returns empty",
			raw:  "   ",
			want: "",
		},
		{
			name: "whitespace trimmed",
			raw:  "  https://github.com/owner/repo.git  ",
			want: "https://github.com/owner/repo",
		},
		{
			name: "credentials stripped from https url",
			raw:  "https://x-access-token:ghp_secret123@github.com/owner/repo.git",
			want: "https://github.com/owner/repo",
		},
		{
			name: "user:pass credentials stripped",
			raw:  "https://user:password@github.com/owner/repo",
			want: "https://github.com/owner/repo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeRemoteURL(tc.raw)
			if got != tc.want {
				t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestComputeRepoID(t *testing.T) {
	id := ComputeRepoID("https://github.com/owner/repo")

	// Must be 16 hex characters.
	if len(id) != 16 {
		t.Fatalf("ComputeRepoID length = %d, want 16", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("ComputeRepoID contains non-hex char %q", string(c))
		}
	}

	// Idempotency: same input → same output.
	id2 := ComputeRepoID("https://github.com/owner/repo")
	if id != id2 {
		t.Errorf("ComputeRepoID not idempotent: %q != %q", id, id2)
	}

	// Different inputs → different IDs.
	other := ComputeRepoID("https://github.com/other/repo")
	if id == other {
		t.Errorf("different inputs produced same ID: %q", id)
	}
}

func TestComputeRepoIDFromPath(t *testing.T) {
	id := ComputeRepoIDFromPath("/home/user/projects/myrepo")

	// Must be 16 hex characters.
	if len(id) != 16 {
		t.Fatalf("ComputeRepoIDFromPath length = %d, want 16", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("ComputeRepoIDFromPath contains non-hex char %q", string(c))
		}
	}

	// Idempotency.
	id2 := ComputeRepoIDFromPath("/home/user/projects/myrepo")
	if id != id2 {
		t.Errorf("ComputeRepoIDFromPath not idempotent: %q != %q", id, id2)
	}

	// Different paths → different IDs.
	other := ComputeRepoIDFromPath("/home/user/projects/other")
	if id == other {
		t.Errorf("different paths produced same ID: %q", id)
	}
}
