package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sagmans/serverpro/internal/privatefile"
)

const StateSchemaVersion = 1

type State struct {
	SchemaVersion int               `json:"schema_version,omitempty"`
	Namespace     string            `json:"namespace"`
	Project       string            `json:"-"`
	Server        string            `json:"server"`
	Compute       ComputeState      `json:"compute"`
	Tailscale     TailscaleState    `json:"tailscale"`
	Cloudflare    CloudflareState   `json:"cloudflare"`
	Ingress       []IngressState    `json:"ingress,omitempty"`
	Labels        map[string]string `json:"labels"`
	Validations   []Validation      `json:"validations,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type ComputeState struct {
	Provider      string            `json:"provider,omitempty"`
	Account       string            `json:"-"`
	Namespace     string            `json:"namespace,omitempty"`
	Server        string            `json:"server,omitempty"`
	ID            string            `json:"id,omitempty"`
	Name          string            `json:"name,omitempty"`
	Location      string            `json:"location,omitempty"`
	Size          string            `json:"size,omitempty"`
	Image         string            `json:"image,omitempty"`
	PublicIPv4    string            `json:"public_ipv4,omitempty"`
	PublicIPv6    string            `json:"public_ipv6,omitempty"`
	ProviderState map[string]string `json:"provider_state,omitempty"`
}

type TailscaleState struct {
	Tailnet                string   `json:"tailnet,omitempty"`
	TailnetID              string   `json:"tailnet_id,omitempty"`
	NodeID                 string   `json:"node_id,omitempty"`
	AuthKeyID              string   `json:"auth_key_id,omitempty"`
	Name                   string   `json:"name,omitempty"`
	IPs                    []string `json:"ips,omitempty"`
	Tags                   []string `json:"tags,omitempty"`
	DeviceBaselineCaptured bool     `json:"device_baseline_captured,omitempty"`
	PreexistingDeviceIDs   []string `json:"preexisting_device_ids,omitempty"`
	PolicyTagOwners        []string `json:"policy_tag_owners,omitempty"`
	PolicySSHRule          bool     `json:"policy_ssh_rule,omitempty"`
	PolicyPendingTagOwners []string `json:"policy_pending_tag_owners,omitempty"`
	PolicyPendingSSHRule   bool     `json:"policy_pending_ssh_rule,omitempty"`
	PolicySSHTags          []string `json:"policy_ssh_tags,omitempty"`
	PolicySSHUser          string   `json:"policy_ssh_user,omitempty"`
}

type CloudflareState struct {
	TunnelID string `json:"tunnel_id,omitempty"`
	Name     string `json:"name,omitempty"`
}

type IngressState struct {
	Type     string `json:"type"`
	Hostname string `json:"hostname"`
	Target   string `json:"target,omitempty"`
	Status   string `json:"status,omitempty"`
}

type Validation struct {
	Time    time.Time `json:"time"`
	Summary string    `json:"summary"`
	Passed  bool      `json:"passed"`
}

func Load(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	if err := validateSchemaVersion("state", s.SchemaVersion, StateSchemaVersion); err != nil {
		return State{}, err
	}
	s.normalize()
	return s, nil
}

func Save(path string, s State) error {
	if err := validateSchemaVersion("state", s.SchemaVersion, StateSchemaVersion); err != nil {
		return err
	}
	now := time.Now().UTC()
	s.normalize()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	return privatefile.AtomicWriteJSON(path, s, privatefile.WriteOptions{TempPattern: ".state-*.tmp"})
}

func (s State) NamespaceName() string {
	if s.Namespace != "" {
		return s.Namespace
	}
	return s.Project
}

func (s *State) normalize() {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = StateSchemaVersion
	}
	if s.Namespace == "" {
		s.Namespace = s.Project
	}
	if s.Project == "" {
		s.Project = s.Namespace
	}
	if s.Compute.Namespace == "" {
		s.Compute.Namespace = s.Namespace
	}
	if s.Compute.Server == "" {
		s.Compute.Server = s.Server
	}
}

func validateSchemaVersion(kind string, version, supported int) error {
	// Rejecting unknown schemas prevents older binaries from silently corrupting newer state.
	if version != 0 && version != supported {
		return fmt.Errorf("unsupported %s schema version %d; supported version is %d", kind, version, supported)
	}
	return nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
