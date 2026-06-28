// Package s3 implements the tiny slice of S3 that auto-artifact needs —
// authenticated PutObject / DeleteObject over a hand-rolled AWS Signature
// Version 4 signer (stdlib crypto only, zero new runtime dependencies, Q-1).
package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// algorithm is the SigV4 algorithm identifier.
	algorithm = "AWS4-HMAC-SHA256"
	// terminator ends the credential scope and the signing-key chain.
	terminator = "aws4_request"
	// unsignedPayload tells S3 not to require a body hash. Valid over HTTPS,
	// which the client enforces, so PutObject can stream the file with no
	// buffering and no second read.
	unsignedPayload = "UNSIGNED-PAYLOAD"
	// emptyPayloadHash is the well-known SHA256 of the empty string, used as
	// the payload hash for bodiless requests (DELETE).
	emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// signingParams carries the inputs the signer needs that are not on the request.
type signingParams struct {
	accessKeyID     string
	secretAccessKey string
	region          string
	service         string
}

// sign computes the SigV4 Authorization header for req and installs it, along
// with the x-amz-date header derived from now. payloadHash is the value used as
// the canonical request's hashed-payload line; callers that send an
// x-amz-content-sha256 header must set it to the same value before calling sign
// (it is then signed as an x-amz-* header). Headers signed are: host,
// content-type (if present), and every header whose name begins with "x-amz-".
//
// sign returns the Authorization header value it set, for testability.
func sign(req *http.Request, payloadHash string, p signingParams, now time.Time) string {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)

	canonicalRequest, signedHeaders := canonicalRequestString(req, payloadHash)

	credentialScope := strings.Join([]string{dateStamp, p.region, p.service, terminator}, "/")

	stringToSign := strings.Join([]string{
		algorithm,
		amzDate,
		credentialScope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveSigningKey(p.secretAccessKey, dateStamp, p.region, p.service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authorization := algorithm +
		" Credential=" + p.accessKeyID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature
	req.Header.Set("Authorization", authorization)
	return authorization
}

// canonicalRequestString assembles the SigV4 canonical request and returns it
// alongside the signed-headers list. payloadHash is used verbatim as the final
// hashed-payload line (e.g. the literal UNSIGNED-PAYLOAD for streamed PUTs).
func canonicalRequestString(req *http.Request, payloadHash string) (canonical, signedHeaders string) {
	canonicalHeaders, signedHeaders := canonicalizeHeaders(req)
	canonical = strings.Join([]string{
		req.Method,
		canonicalURI(req),
		canonicalQuery(req),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	return canonical, signedHeaders
}

// canonicalizeHeaders builds the canonical headers block and the signed-headers
// list. Host comes from the URL (Go does not keep it in req.Header). Beyond
// host it signs content-type (if set) and every x-amz-* header present.
func canonicalizeHeaders(req *http.Request) (canonical string, signed string) {
	values := map[string]string{"host": req.URL.Host}
	for name, vs := range req.Header {
		lower := strings.ToLower(name)
		if lower == "content-type" || strings.HasPrefix(lower, "x-amz-") {
			values[lower] = trimHeaderValue(strings.Join(vs, ","))
		}
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(values[name])
		b.WriteByte('\n')
	}
	return b.String(), strings.Join(names, ";")
}

// canonicalURI returns the URI-encoded path. The request's EscapedPath() is set
// by the client to the AWS-style encoding, so RequestURI's path matches what
// goes on the wire byte-for-byte.
func canonicalURI(req *http.Request) string {
	path := req.URL.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

// canonicalQuery returns the canonical (sorted, encoded) query string. The
// client makes no query-bearing calls, so this is normally empty.
func canonicalQuery(req *http.Request) string {
	q := req.URL.Query()
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := q[k]
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// trimHeaderValue trims surrounding whitespace and collapses internal runs of
// spaces, per the SigV4 canonical-header rules.
func trimHeaderValue(v string) string {
	return strings.Join(strings.Fields(v), " ")
}

// uriEncode percent-encodes s per RFC 3986, leaving the unreserved set
// (A-Z a-z 0-9 - _ . ~) untouched. When encodeSlash is false, '/' passes
// through so it can be used to encode a path while preserving segment
// separators — matching AWS's reference uriEncode.
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte('/')
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0x0f])
		}
	}
	return b.String()
}

const upperHex = "0123456789ABCDEF"

func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte(terminator))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
