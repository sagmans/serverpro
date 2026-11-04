package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/tailscale/hujson"
)

// policyMediaHuJSON asks the policy API for the file as the operator wrote
// it. A JSON read strips comments, and a JSON post then deletes them.
const policyMediaHuJSON = "application/hujson"

// policyMediaJSON asks for the validation result, which is plain JSON.
const policyMediaJSON = "application/json"

var (
	ErrPolicyETagMissing = errors.New("tailnet policy response missing ETag; refusing update")
	ErrPolicyNotObject   = errors.New("tailnet policy file is not a JSON object; refusing update")
)

// policyFile is one read of the tailnet policy. source keeps the operator's
// HuJSON text for renderPolicyFile; doc is its standard JSON view, which the
// tag-owner and SSH-rule helpers edit; etag guards the later post.
type policyFile struct {
	source hujson.Value
	doc    policyDocument
	etag   string
}

func (c Client) policyPath() string {
	return "/tailnet/" + url.PathEscape(c.tailnet) + "/acl"
}

func (c Client) readPolicyFile(ctx context.Context) (policyFile, error) {
	b, etag, err := c.doPolicyRequest(ctx, http.MethodGet, c.policyPath(), nil, "", policyMediaHuJSON, "")
	if err != nil {
		return policyFile{}, err
	}
	source, err := hujson.Parse(b)
	if err != nil {
		return policyFile{}, fmt.Errorf("parse tailnet policy HuJSON: %w", err)
	}
	var doc policyDocument
	if err := json.Unmarshal(standardJSON(source), &doc); err != nil {
		return policyFile{}, fmt.Errorf("decode tailnet policy: %w", err)
	}
	// A null file decodes to a nil map; the edit helpers would panic on it and
	// the diff could not patch a non-object root, so refuse it here.
	if doc == nil {
		return policyFile{}, ErrPolicyNotObject
	}
	return policyFile{source: source, doc: doc, etag: etag}, nil
}

// publishPolicyFile validates and posts the exact bytes it renders, so the
// policy that Tailscale checks is the policy that it stores.
func (c Client) publishPolicyFile(ctx context.Context, file policyFile) error {
	if file.etag == "" {
		return ErrPolicyETagMissing
	}
	body, err := renderPolicyFile(file)
	if err != nil {
		return err
	}
	if err := c.validatePolicyBody(ctx, body); err != nil {
		return err
	}
	_, _, err = c.doPolicyRequest(ctx, http.MethodPost, c.policyPath(), body, file.etag, policyMediaHuJSON, policyMediaHuJSON)
	return err
}

// validatePolicyBody is the gate before every policy write. Tailscale
// answers /acl/validate with HTTP 200 for failed tests and for rejected
// syntax too, so the status alone proves nothing; only the body decides.
// The request body is HuJSON, but the answer is a JSON result, so it asks
// for JSON back.
func (c Client) validatePolicyBody(ctx context.Context, body []byte) error {
	res, _, err := c.doPolicyRequest(ctx, http.MethodPost, c.policyPath()+"/validate", body, "", policyMediaJSON, policyMediaHuJSON)
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

func (c Client) doPolicyRequest(ctx context.Context, method, path string, body []byte, ifMatch, accept, contentType string) ([]byte, string, error) {
	headers := http.Header{}
	if accept != "" {
		headers.Set("Accept", accept)
	}
	if ifMatch != "" {
		headers.Set("If-Match", ifMatch)
	}
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	b, resHeaders, err := c.api.DoRaw(ctx, method, path, body, headers)
	if err != nil {
		return nil, "", err
	}
	return b, resHeaders.Get("ETag"), nil
}
