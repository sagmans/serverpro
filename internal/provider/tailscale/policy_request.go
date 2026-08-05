package tailscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

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

func (c Client) validatePolicyDocument(ctx context.Context, doc policyDocument) error {
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, _, err = c.doPolicyRequest(ctx, http.MethodPost, "/tailnet/"+url.PathEscape(c.tailnet)+"/acl/validate", b, "", "application/json")
	return err
}

func (c Client) postPolicyDocument(ctx context.Context, doc policyDocument, etag string) error {
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
