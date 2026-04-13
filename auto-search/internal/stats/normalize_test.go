package stats

import "testing"

func TestNormalizeBashCommandFamily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "chain picks last segment",
			in:   "cd auto-etl && go build -o ../bin/autoetl .",
			want: "go build",
		},
		{
			name: "env prefix stripped",
			in:   "FOO=1 BAR=2 go test ./...",
			want: "go test",
		},
		{
			name: "sudo env prefixes stripped",
			in:   "sudo env GOFLAGS=-count=1 go test ./...",
			want: "go test",
		},
		{
			name: "bash lc unwrapped",
			in:   "bash -lc 'go vet ./...'",
			want: "go vet",
		},
		{
			name: "sh lc unwrapped",
			in:   `sh -lc "go test ./..."`,
			want: "go test",
		},
		{
			name: "single token command",
			in:   "make",
			want: "make",
		},
		{
			name: "empty after prefix stripping",
			in:   "env FOO=1 BAR=2",
			want: emptyBucketKey,
		},
		{
			name: "blank input",
			in:   "   ",
			want: emptyBucketKey,
		},
		{
			name: "chain with semicolon and pipe preserved",
			in:   `echo ok; go test ./... | tee out.txt`,
			want: "go test",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeBashCommandFamily(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeBashCommandFamily(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeBucketValueEmpty(t *testing.T) {
	t.Parallel()
	if got := normalizeBucketValue("  "); got != emptyBucketKey {
		t.Fatalf("normalizeBucketValue empty = %q, want %q", got, emptyBucketKey)
	}
}
