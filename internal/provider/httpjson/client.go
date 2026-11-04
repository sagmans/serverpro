package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const maxResponseBodyBytes int64 = 1 << 20

// Error text from a failed call ends up in doctor evidence, create output,
// and logs, so it names the call without the account, zone, or tunnel it
// targeted and keeps only the start of the provider's answer: enough to read
// the reason, not a dump of whatever the API sent back.
const (
	// maxErrorBodyBytes bounds the response text an error repeats; provider
	// error objects put their code and message first.
	maxErrorBodyBytes = 256
	// redactedID replaces an opaque identifier in error text.
	redactedID = "{id}"
	// querySeparator starts a query string, whose values can carry names.
	querySeparator = "?"
)

// opaqueIDPattern matches the account-wide identifiers that API paths and
// messages carry: Cloudflare account and zone IDs are 32 hex digits, tunnel
// IDs are UUIDs. A short numeric server ID names only one resource and stays,
// so the error still says which server failed.
var opaqueIDPattern = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}|[0-9A-Fa-f]{32})\b`)

// Redirect chains are the one channel where an origin can launder a request toward a
// second destination while we still hold bearer tokens and create payloads, so every
// hop must stay inside the credential's trust boundary.
const (
	maxRedirectHops = 5
)

type redirectGuardKey struct{}

// redirectGuard lets the policy see what the ORIGINAL request carried without trusting
// the hop-rewritten request itself (body replay detection would otherwise be ambiguous).
type redirectGuard struct {
	hasBody bool
}

// DefaultCheckRedirect refuses any hop that could disclose credentials to a party the
// first request never consented to: plaintext downgrades, other hosts (subdomains and
// other ports included), oversized loops, and 307/308 replays of secret-bearing bodies.
func DefaultCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirectHops {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	first := via[0]
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing non-https redirect target %q", req.URL.Redacted())
	}
	if req.URL.Host != first.URL.Host {
		return fmt.Errorf("refusing cross-host redirect from %q to %q", first.URL.Host, req.URL.Host)
	}
	if g, ok := req.Context().Value(redirectGuardKey{}).(*redirectGuard); ok && g.hasBody && req.Body != nil {
		return errors.New("refusing request-body replay across a 307/308 redirect")
	}
	return nil
}

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

type BodyTooLargeError struct {
	Limit int64
}

func (e *BodyTooLargeError) Error() string {
	return fmt.Sprintf("response body exceeds %d bytes", e.Limit)
}

// StatusError keeps the full path and body for callers that classify a
// failure, such as an active-connections refusal; only its text is cut down.
type StatusError struct {
	Method     string
	Path       string
	Status     string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s failed: %s: %s", e.Method, redactPath(e.Path), e.Status, shortenBody(e.Body))
}

// RedactIDs masks the account, zone, and tunnel IDs in text. It is exported
// so callers that build their own error text about a provider resource keep
// the IDs out of it the same way failed calls do.
func RedactIDs(text string) string {
	return opaqueIDPattern.ReplaceAllString(text, redactedID)
}

// redactPath drops the query and masks opaque IDs, so the text still names
// the endpoint that failed.
func redactPath(path string) string {
	path, _, _ = strings.Cut(path, querySeparator)
	return RedactIDs(path)
}

// shortenBody masks opaque IDs, folds whitespace so an HTML error page stays
// on one line, and keeps at most maxErrorBodyBytes on a UTF-8 boundary,
// saying how long the whole answer was.
func shortenBody(body string) string {
	text := strings.Join(strings.Fields(RedactIDs(body)), " ")
	if len(text) <= maxErrorBodyBytes {
		return text
	}
	cut := maxErrorBodyBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return fmt.Sprintf("%s... (%d bytes)", text[:cut], len(body))
}

func IsStatus(err error, code int) bool {
	var statusErr *StatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == code
}

func (c Client) Do(ctx context.Context, method, path string, in, out any) error {
	b, err := c.DoBytes(ctx, method, path, in)
	if err != nil {
		return err
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

func (c Client) DoBytes(ctx context.Context, method, path string, in any) ([]byte, error) {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = b
	}
	out, _, err := c.DoRaw(ctx, method, path, body, nil)
	return out, err
}

func (c Client) DoRaw(ctx context.Context, method, path string, body []byte, headers http.Header) ([]byte, http.Header, error) {
	var in io.Reader
	if body != nil {
		in = bytes.NewReader(body)
	}
	// The guard rides the context so the redirect policy compares against what was
	// originally sent rather than whatever each hop rewrote the request into.
	ctx = context.WithValue(ctx, redirectGuardKey{}, &redirectGuard{hasBody: body != nil})
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, in)
	if err != nil {
		return nil, nil, err
	}
	// A caller-set Content-Type wins: the Tailscale policy API takes HuJSON,
	// and a second header value would make the media type ambiguous.
	if body != nil && headers.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	if h.CheckRedirect == nil {
		// Shallow copy shares transport/jar by design; only the redirect hook changes.
		secure := *h
		secure.CheckRedirect = DefaultCheckRedirect
		h = &secure
	}
	res, err := h.Do(req)
	if err != nil {
		// A transport failure prints the request URL; mask it the same way,
		// keeping the cause chain for timeout and cancellation checks.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = redactPath(urlErr.URL)
		}
		return nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBodyBytes+1))
	if err != nil {
		return nil, nil, err
	}
	// Read one sentinel byte so a complete limit-sized body is not mistaken for truncation.
	if int64(len(b)) > maxResponseBodyBytes {
		return nil, nil, &BodyTooLargeError{Limit: maxResponseBodyBytes}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, nil, &StatusError{Method: method, Path: path, Status: res.Status, StatusCode: res.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return b, res.Header.Clone(), nil
}
