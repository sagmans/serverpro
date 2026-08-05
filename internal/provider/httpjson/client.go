package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBodyBytes int64 = 1 << 20

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

type StatusError struct {
	Method     string
	Path       string
	Status     string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s failed: %s: %s", e.Method, e.Path, e.Status, e.Body)
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
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, in)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
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
	res, err := h.Do(req)
	if err != nil {
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
