package graph

import "testing"

func TestHashBytes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// The SHA-256 of the empty input, the hash a zero-byte annex file gets.
		{"empty", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		// A fixed, well-known vector so the hex is stable across runs.
		{"abc", "abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HashBytes([]byte(tc.in))
			if got != tc.want {
				t.Fatalf("HashBytes(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if len(got) != 64 {
				t.Fatalf("HashBytes(%q) length = %d, want 64", tc.in, len(got))
			}
		})
	}
}
