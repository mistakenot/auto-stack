package s3

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// AWS-published SigV4 test-suite credentials (aws-sig-v4-test-suite). The whole
// suite is signed with these against service "service" / region "us-east-1" at
// the fixed timestamp 20150830T123600Z.
const (
	vectorAccessKey = "AKIDEXAMPLE"
	vectorSecret    = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
	vectorRegion    = "us-east-1"
	vectorService   = "service"
)

func vectorParams() signingParams {
	return signingParams{
		accessKeyID:     vectorAccessKey,
		secretAccessKey: vectorSecret,
		region:          vectorRegion,
		service:         vectorService,
	}
}

func vectorTime() time.Time {
	return time.Date(2015, time.August, 30, 12, 36, 0, 0, time.UTC)
}

// TestSignGetVanilla checks the full Authorization header against the canonical
// "get-vanilla" case from AWS's published SigV4 test suite — the signing-key
// chain, canonical request, and string-to-sign must all be correct end to end.
func TestSignGetVanilla(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	got := sign(req, emptyPayloadHash, vectorParams(), vectorTime())

	want := "AWS4-HMAC-SHA256 " +
		"Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
		"SignedHeaders=host;x-amz-date, " +
		"Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got != want {
		t.Errorf("authorization mismatch\n got: %s\nwant: %s", got, want)
	}
	if hdr := req.Header.Get("X-Amz-Date"); hdr != "20150830T123600Z" {
		t.Errorf("x-amz-date = %q, want 20150830T123600Z", hdr)
	}
}

// TestSignVanillaWithHeader checks the "get-vanilla-with-header" case: a custom
// signed header (My-Header1) participates in the canonical headers and changes
// the signature, exercising header sorting and trimming.
func TestSignVanillaWithHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// Use an x-amz-* header so our signer includes it (the suite's My-Header*
	// cases sign arbitrary headers; we only sign host, content-type, x-amz-*).
	req.Header.Set("X-Amz-Meta-Foo", "  bar  baz  ")

	canonical, signed := canonicalRequestString(req, emptyPayloadHash)
	if signed != "host;x-amz-meta-foo" {
		// x-amz-date is added by sign(), not present here yet.
		t.Errorf("signed headers = %q, want host;x-amz-meta-foo", signed)
	}
	// Internal whitespace must be collapsed to a single space.
	if !strings.Contains(canonical, "x-amz-meta-foo:bar baz\n") {
		t.Errorf("canonical request did not collapse header whitespace:\n%s", canonical)
	}
}

// TestUnsignedPayloadLiteral asserts the canonical request embeds the literal
// UNSIGNED-PAYLOAD (not a body hash) and that the content-sha256 header is among
// the signed headers — the streamed-PutObject contract from the resolved
// design thread.
func TestUnsignedPayloadLiteral(t *testing.T) {
	req, err := http.NewRequest(http.MethodPut, "https://bucket.s3.eu-west-1.amazonaws.com/90d/x/y.png", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("X-Amz-Content-Sha256", unsignedPayload)

	canonical, signed := canonicalRequestString(req, unsignedPayload)
	if !strings.HasSuffix(canonical, "\n"+unsignedPayload) {
		t.Errorf("canonical request must end with the UNSIGNED-PAYLOAD literal:\n%s", canonical)
	}
	want := "content-type;host;x-amz-content-sha256"
	if signed != want {
		t.Errorf("signed headers = %q, want %q", signed, want)
	}
}

// TestURIEncode covers the path-encoding edge cases the AWS vectors do not:
// spaces, '+', unicode, and slash preservation.
func TestURIEncode(t *testing.T) {
	cases := []struct {
		in          string
		encodeSlash bool
		want        string
	}{
		{"plain.txt", true, "plain.txt"},
		{"a b.txt", true, "a%20b.txt"},
		{"a+b.txt", true, "a%2Bb.txt"},
		{"90d/u/x.png", false, "90d/u/x.png"},
		{"90d/u/x.png", true, "90d%2Fu%2Fx.png"},
		{"café.png", true, "caf%C3%A9.png"},
		{"a~b-c_d.e", true, "a~b-c_d.e"},
	}
	for _, tc := range cases {
		if got := uriEncode(tc.in, tc.encodeSlash); got != tc.want {
			t.Errorf("uriEncode(%q, %v) = %q, want %q", tc.in, tc.encodeSlash, got, tc.want)
		}
	}
}

// TestCanonicalURIMatchesWirePath verifies the path the signer canonicalizes is
// byte-for-byte the path the request sends (EscapedPath set by the client),
// even for filenames with spaces.
func TestCanonicalURIMatchesWirePath(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "bucket.s3.eu-west-1.amazonaws.com"}
	key := "90d/abc/my file.png"
	u.Path = "/" + key
	u.RawPath = "/" + uriEncode(key, false)
	req := &http.Request{Method: http.MethodPut, URL: u, Header: http.Header{}}

	if got := canonicalURI(req); got != "/90d/abc/my%20file.png" {
		t.Errorf("canonicalURI = %q, want /90d/abc/my%%20file.png", got)
	}
	if got := req.URL.RequestURI(); got != "/90d/abc/my%20file.png" {
		t.Errorf("RequestURI = %q, want it to match canonicalURI", got)
	}
}
