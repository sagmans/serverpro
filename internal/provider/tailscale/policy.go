package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/tailscale/hujson"
)

func (c Client) Policy(ctx context.Context) (Policy, error) {
	var p Policy
	b, err := c.api.DoBytes(ctx, http.MethodGet, "/tailnet/"+url.PathEscape(c.tailnet)+"/acl", nil)
	if err != nil {
		return p, err
	}
	v, err := hujson.Parse(b)
	if err != nil {
		return p, err
	}
	v.Standardize()
	if err := json.Unmarshal(v.Pack(), &p); err != nil {
		return p, err
	}
	return p, nil
}

type ServerproPolicyChange struct {
	TagOwners []string
	SSHRule   bool
}

type PolicyCheckpoint func(ServerproPolicyChange) error

func (c Client) EnsureServerproPolicy(ctx context.Context, tags []string, adminUser, rootPolicy string, checkpoint PolicyCheckpoint) (ServerproPolicyChange, error) {
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
		if checkpoint != nil {
			if err := checkpoint(change); err != nil {
				return change, fmt.Errorf("checkpoint tailscale policy ownership: %w", err)
			}
		}
		return change, nil
	}
	if err := c.validatePolicyDocument(ctx, doc); err != nil {
		return change, err
	}
	// Pending ownership must be durable before policy mutation. Callers promote
	// it only after this conditional write succeeds; ambiguous failures stay pending.
	if checkpoint != nil {
		if err := checkpoint(change); err != nil {
			return change, fmt.Errorf("checkpoint tailscale policy ownership: %w", err)
		}
	}
	if err := c.postPolicyDocument(ctx, doc, etag); err != nil {
		return change, err
	}
	return change, nil
}

func (c Client) InspectServerproPolicyParts(ctx context.Context, tagOwnerTags, sshTags []string, adminUser string) (ServerproPolicyChange, error) {
	var present ServerproPolicyChange
	doc, _, err := c.policyDocument(ctx)
	if err != nil {
		return present, err
	}
	present.TagOwners, err = doc.inspectTagOwners(tagOwnerTags)
	if err != nil {
		return present, err
	}
	if len(sshTags) > 0 {
		present.SSHRule, err = doc.inspectSSHRule(sshTags, adminUser)
	}
	return present, err
}

func (c Client) RemoveServerproPolicy(ctx context.Context, tags []string, adminUser string, removeSSH bool) (bool, error) {
	sshTags := []string(nil)
	if removeSSH {
		sshTags = tags
	}
	return c.RemoveServerproPolicyParts(ctx, tags, sshTags, adminUser)
}

func (c Client) RemoveServerproPolicyParts(ctx context.Context, tagOwnerTags, sshTags []string, adminUser string) (bool, error) {
	if len(tagOwnerTags) == 0 && len(sshTags) == 0 {
		return false, nil
	}
	doc, etag, err := c.policyDocument(ctx)
	if err != nil {
		return false, err
	}
	changedTags, err := doc.removeTagOwners(tagOwnerTags)
	if err != nil {
		return false, err
	}
	changedSSH := false
	if len(sshTags) > 0 {
		changedSSH, err = doc.removeSSHRule(sshTags, adminUser)
		if err != nil {
			return false, err
		}
	}
	if !changedTags && !changedSSH {
		return false, nil
	}
	if err := c.validatePolicyDocument(ctx, doc); err != nil {
		return false, err
	}
	if err := c.postPolicyDocument(ctx, doc, etag); err != nil {
		return false, err
	}
	return true, nil
}
