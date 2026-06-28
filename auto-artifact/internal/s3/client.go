package s3

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mistakenot/auto-artifact/internal/config"
)

// Client makes the small set of authenticated S3 calls auto-artifact needs:
// PutObject, DeleteObject, and a write/delete Probe for `doctor`. Reads are
// never signed — objects are served by the public bucket policy.
type Client struct {
	httpClient *http.Client
	params     signingParams
	scheme     string
	host       string // virtual-hosted host: {bucket}.{endpoint-host}
	bucket     string
	region     string
	now        func() time.Time
}

// NewClient binds credentials and builds the virtual-hosted base URL from the
// configured endpoint and bucket. The endpoint must be HTTPS — the tool never
// emits or signs against http:// (requirements: HTTPS-only).
func NewClient(cfg config.Settings) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint %q: %w", cfg.Endpoint, err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("endpoint must be https (got %q)", cfg.Endpoint)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("endpoint %q has no host", cfg.Endpoint)
	}
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Minute},
		params: signingParams{
			accessKeyID:     cfg.AccessKeyID,
			secretAccessKey: cfg.SecretAccessKey,
			region:          cfg.Region,
			service:         "s3",
		},
		scheme: u.Scheme,
		host:   cfg.Bucket + "." + u.Host,
		bucket: cfg.Bucket,
		region: cfg.Region,
		now:    time.Now,
	}, nil
}

// PublicURL returns the permanent public HTTPS URL for an object key.
func (c *Client) PublicURL(key string) string {
	return c.scheme + "://" + c.host + "/" + uriEncode(key, false)
}

// objectURL builds the request URL for key so the wire path matches the path
// the signer canonicalizes byte-for-byte (RawPath = AWS-style encoding).
func (c *Client) objectURL(key string) *url.URL {
	return &url.URL{
		Scheme:  c.scheme,
		Host:    c.host,
		Path:    "/" + key,
		RawPath: "/" + uriEncode(key, false),
	}
}

// PutObject uploads body of the given size under key with the supplied
// Content-Type. It signs with x-amz-content-sha256: UNSIGNED-PAYLOAD (valid over
// the enforced-HTTPS endpoint) so the body streams straight from the caller's
// open file — no buffering, no second read, no OOM on large files.
func (c *Client) PutObject(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "", body)
	if err != nil {
		return err
	}
	req.URL = c.objectURL(key)
	req.Host = c.host
	req.ContentLength = size
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Amz-Content-Sha256", unsignedPayload)
	sign(req, unsignedPayload, c.params, c.now())

	return c.do(req, "upload "+key)
}

// DeleteObject removes key. The empty-body payload hash is signed. S3's
// DeleteObject returns 204 even when the object does not exist, so success is
// 200/204 only; a 404 here means NoSuchBucket (wrong bucket/endpoint) and is
// surfaced as an error rather than reported as a successful delete.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "", nil)
	if err != nil {
		return err
	}
	req.URL = c.objectURL(key)
	req.Host = c.host
	req.Header.Set("X-Amz-Content-Sha256", emptyPayloadHash)
	sign(req, emptyPayloadHash, c.params, c.now())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}
	return c.statusError(resp, "delete "+key)
}

// Probe verifies real write/delete permission by putting then deleting a tiny
// object under 7d/.doctor/<uuid> (D-5). The IAM user lacks ListBucket/GetObject,
// so a HeadBucket/HeadObject probe would false-negative; PUT+DELETE exercises
// exactly the permissions the tool uses and self-cleans (7d auto-expiry if the
// delete somehow fails).
func (c *Client) Probe(ctx context.Context) error {
	id, err := randomHex(8)
	if err != nil {
		return err
	}
	key := "7d/.doctor/" + id
	body := []byte("doctor probe")
	if err := c.PutObject(ctx, key, strings.NewReader(string(body)), int64(len(body)), "text/plain"); err != nil {
		return err
	}
	return c.DeleteObject(ctx, key)
}

func (c *Client) do(req *http.Request, action string) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return c.statusError(resp, action)
}

// statusError maps a non-2xx S3 response to a remediation-carrying error,
// including a snippet of the (XML) error body.
func (c *Client) statusError(resp *http.Response, action string) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	hint := "check credentials and bucket config — run `auto artifact doctor`"
	if resp.StatusCode == http.StatusForbidden {
		hint = "access denied — verify access_key_id/secret_access_key and the bucket policy; run `auto artifact doctor`"
	}
	return fmt.Errorf("%s: S3 returned %d: %s (%s)", action, resp.StatusCode, strings.TrimSpace(string(snippet)), hint)
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
