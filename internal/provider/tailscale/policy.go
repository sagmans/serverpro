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

// SSHPolicy reads only the ssh section for doctor's rule comparison.
func (c Client) SSHPolicy(ctx context.Context) ([]mesh.SSHRule, error) {
	p, err := c.Policy(ctx)
	return p.SSH, err
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

// EnsureServerproPolicy writes the serverpro tag owners, the human check rule
// and, when automationSource is set, the automation accept rule. An empty
// automationSource leaves an existing accept rule alone: servers that share
// the same tags and admin user share that rule, so one server with automation
// off must not revoke it for the others. Revocation is RemoveAutomationSSHRule,
// which only the caller that checked the registry may call.
func (c Client) EnsureServerproPolicy(ctx context.Context, tags []string, adminUser, rootPolicy, automationSource string) (ServerproPolicyChange, error) {
	var change ServerproPolicyChange
	if rootPolicy != "check-or-disabled" {
		return change, fmt.Errorf("unsupported root policy %q", rootPolicy)
	}
	if automationSource != "" && automationSource != mesh.AutomationSourceTag {
		return change, fmt.Errorf("unsupported automation SSH source %q: only %s is allowed", automationSource, mesh.AutomationSourceTag)
	}
	// Refused before the policy read: the read-back runs only after the
	// write, so a root accept rule would already be live when it failed.
	if automationSource != "" && adminUser == mesh.RootSSHUser {
		return change, fmt.Errorf("refused automation SSH accept rule for %s: root must stay check or disabled", mesh.RootSSHUser)
	}
	doc, etag, err := c.policyDocument(ctx)
	if err != nil {
		return change, err
	}
	ownedTags := append([]string(nil), tags...)
	if automationSource != "" {
		ownedTags = append(ownedTags, automationSource)
	}
	addedTags, err := doc.ensureTagOwners(ownedTags)
	if err != nil {
		return change, err
	}
	change.TagOwners = addedTags
	addedSSHRule, err := doc.ensureSSHRule(tags, adminUser)
	if err != nil {
		return change, err
	}
	change.SSHRule = addedSSHRule
	change.AutomationSSHRule, change.AutomationSSHRuleRemoved, err = doc.ensureAutomationSSHRule(tags, adminUser, automationSource)
	if err != nil {
		return change, err
	}
	return change, c.publishPolicyChange(ctx, doc, etag, change)
}

// RemoveAutomationSSHRule revokes the automation accept rule for these tags
// and admin user. The caller must first make sure that no other registered
// server records the same rule, because the rule is shared by tag set.
func (c Client) RemoveAutomationSSHRule(ctx context.Context, tags []string, adminUser string) (ServerproPolicyChange, error) {
	var change ServerproPolicyChange
	doc, etag, err := c.policyDocument(ctx)
	if err != nil {
		return change, err
	}
	change.AutomationSSHRuleRemoved, err = doc.removeAutomationSSHRule(tags, adminUser)
	if err != nil {
		return change, err
	}
	return change, c.publishPolicyChange(ctx, doc, etag, change)
}

// publishPolicyChange validates and posts a document only when it changed, so
// a run against a policy that already matches makes no write and keeps the
// operator's HuJSON comments.
func (c Client) publishPolicyChange(ctx context.Context, doc policyDocument, etag string, change ServerproPolicyChange) error {
	if !change.Changed() {
		return nil
	}
	if err := c.validatePolicyDocument(ctx, doc); err != nil {
		return err
	}
	return c.postPolicyDocument(ctx, doc, etag)
}
