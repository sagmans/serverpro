package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

var ErrPolicyETagMissing = errors.New("tailnet policy response missing ETag; refusing update")

func (c Client) policyDocument(ctx context.Context) (policyDocument, string, error) {
	b, etag, err := c.doPolicyRequest(ctx, http.MethodGet, "/tailnet/"+url.PathEscape(c.tailnet)+"/acl", nil, "", "application/json")
	if err != nil {
		return nil, "", err
	}
	var doc policyDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, "", err
	}
	return doc, etag, nil
}

// validatePolicyDocument is the gate before every policy write. Tailscale
// answers /acl/validate with HTTP 200 for failed tests and for rejected
// syntax too, so the status alone proves nothing; only the body decides.
func (c Client) validatePolicyDocument(ctx context.Context, doc policyDocument) error {
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	res, _, err := c.doPolicyRequest(ctx, http.MethodPost, "/tailnet/"+url.PathEscape(c.tailnet)+"/acl/validate", b, "", "application/json")
	if err != nil {
		return err
	}
	return parsePolicyValidation(res)
}

// parsePolicyValidation accepts only the empty JSON object that Tailscale
// sends for a policy that passes. Any other shape, including an empty or
// unparseable body, fails closed so a proxy or API change cannot turn a
// rejected policy into a write.
func parsePolicyValidation(body []byte) error {
	// A raw key map, not the typed response, decides the pass: JSON null and
	// objects with unknown or empty fields decode to a zero struct too.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return &PolicyValidationError{Message: errPolicyValidationUnreadable, cause: err}
	}
	if fields != nil && len(fields) == 0 {
		return nil
	}
	var res policyValidationResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return &PolicyValidationError{Message: errPolicyValidationUnreadable, cause: err}
	}
	if res.Message == "" {
		res.Message = errPolicyValidationUnexpected
	}
	return &PolicyValidationError{Message: res.Message, Failures: res.Data}
}

func (c Client) postPolicyDocument(ctx context.Context, doc policyDocument, etag string) error {
	if etag == "" {
		return ErrPolicyETagMissing
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, _, err = c.doPolicyRequest(ctx, http.MethodPost, "/tailnet/"+url.PathEscape(c.tailnet)+"/acl", b, etag, "application/json")
	return err
}

func (c Client) doPolicyRequest(ctx context.Context, method, path string, body []byte, ifMatch, accept string) ([]byte, string, error) {
	headers := http.Header{}
	if accept != "" {
		headers.Set("Accept", accept)
	}
	if ifMatch != "" {
		headers.Set("If-Match", ifMatch)
	}
	b, resHeaders, err := c.api.DoRaw(ctx, method, path, body, headers)
	if err != nil {
		return nil, "", err
	}
	return b, resHeaders.Get("ETag"), nil
}
