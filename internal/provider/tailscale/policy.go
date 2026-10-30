package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/tailscale/hujson"
)

func (c Client) Policy(ctx context.Context) (Policy, error) {
	var p Policy
	err := c.decodePolicyFile(ctx, &p)
	return p, err
}

// PacketPolicy reads the acls and grants sections so doctor can flag an
// allow-all tailnet. It shares Policy's read so both see the same HuJSON
// normalization; it is read-only and never needs the policy ETag.
func (c Client) PacketPolicy(ctx context.Context) (mesh.PacketPolicy, error) {
	var p mesh.PacketPolicy
	err := c.decodePolicyFile(ctx, &p)
	return p, err
}

// decodePolicyFile standardizes the HuJSON policy file (comments, trailing
// commas) before decoding, because the admin console stores it that way.
func (c Client) decodePolicyFile(ctx context.Context, into any) error {
	b, err := c.api.DoBytes(ctx, http.MethodGet, "/tailnet/"+url.PathEscape(c.tailnet)+"/acl", nil)
	if err != nil {
		return err
	}
	v, err := hujson.Parse(b)
	if err != nil {
		return err
	}
	v.Standardize()
	return json.Unmarshal(v.Pack(), into)
}

type ServerproPolicyChange = mesh.PolicyChange

func (c Client) EnsureServerproPolicy(ctx context.Context, tags []string, adminUser, rootPolicy string) (ServerproPolicyChange, error) {
	var change ServerproPolicyChange
	if rootPolicy != "check-or-disabled" {
		return change, fmt.Errorf("unsupported root policy %q", rootPolicy)
	}
	doc, etag, err := c.policyDocument(ctx)
	if err != nil {
		return change, err
	}
	addedTags, err := doc.ensureTagOwners(tags)
	if err != nil {
		return change, err
	}
	change.TagOwners = addedTags
	addedSSHRule, err := doc.ensureSSHRule(tags, adminUser)
	if err != nil {
		return change, err
	}
	change.SSHRule = addedSSHRule
	if len(change.TagOwners) == 0 && !change.SSHRule {
		return change, nil
	}
	if err := c.validatePolicyDocument(ctx, doc); err != nil {
		return change, err
	}
	if err := c.postPolicyDocument(ctx, doc, etag); err != nil {
		return change, err
	}
	return change, nil
}
