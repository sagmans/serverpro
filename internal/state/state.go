package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/privatefile"
)

type State struct {
	SchemaVersion int               `json:"schema_version,omitempty"`
	Namespace     string            `json:"namespace"`
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
	Provider         string                       `json:"provider,omitempty"`
	Account          string                       `json:"-"`
	Namespace        string                       `json:"namespace,omitempty"`
	Server           string                       `json:"server,omitempty"`
	ID               string                       `json:"id,omitempty"`
	Name             string                       `json:"name,omitempty"`
	Location         string                       `json:"location,omitempty"`
	Size             string                       `json:"size,omitempty"`
	Image            string                       `json:"image,omitempty"`
	PublicIPv4       string                       `json:"public_ipv4,omitempty"`
	PublicIPv6       string                       `json:"public_ipv6,omitempty"`
	ManagedResources []compute.ManagedResourceRef `json:"managed_resources,omitempty"`
	ProviderState    map[string]string            `json:"provider_state,omitempty"`
}

type TailscaleState struct {
	Tailnet         string   `json:"tailnet,omitempty"`
	NodeID          string   `json:"node_id,omitempty"`
	AuthKeyID       string   `json:"auth_key_id,omitempty"`
	Name            string   `json:"name,omitempty"`
	IPs             []string `json:"ips,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	PolicyTagOwners []string `json:"policy_tag_owners,omitempty"`
	PolicySSHRule   bool     `json:"policy_ssh_rule,omitempty"`
	PolicySSHTags   []string `json:"policy_ssh_tags,omitempty"`
}

type CloudflareTunnelProvenance string

const (
	CloudflareTunnelCreated  CloudflareTunnelProvenance = "created"
	CloudflareTunnelAdopted  CloudflareTunnelProvenance = "adopted"
	CloudflareTunnelImported CloudflareTunnelProvenance = "imported"
)

type CloudflareState struct {
	TunnelID   string                     `json:"tunnel_id,omitempty"`
	Name       string                     `json:"name,omitempty"`
	Provenance CloudflareTunnelProvenance `json:"provenance,omitempty"`
}

func (s CloudflareState) OwnsTunnel() bool {
	// Missing provenance belongs to legacy state, so fail closed instead of
	// deleting a tunnel whose creation cannot be proven.
	return s.TunnelID != "" && s.Provenance == CloudflareTunnelCreated
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
	var file struct {
		State
		Project string `json:"project"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return State{}, err
	}
	s := file.State
	if s.Namespace != "" && file.Project != "" && s.Namespace != file.Project {
		return State{}, fmt.Errorf("state namespace %q conflicts with legacy project %q", s.Namespace, file.Project)
	}
	if s.Namespace == "" {
		s.Namespace = file.Project
	}
	if err := s.normalize(); err != nil {
		return State{}, err
	}
	return s, nil
}

func Save(path string, s State) error {
	unlock, err := lockState(path)
	if err != nil {
		return err
	}
	defer unlock()
	return saveUnlocked(path, s)
}

func Update(path string, fn func(*State) error) error {
	unlock, err := lockState(path)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := Load(path)
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	return saveUnlocked(path, st)
}

func saveUnlocked(path string, s State) error {
	now := time.Now().UTC()
	if err := s.normalize(); err != nil {
		return err
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	return privatefile.AtomicWriteJSON(path, s, privatefile.WriteOptions{TempPattern: ".state-*.tmp", Sync: true})
}

func (s State) NamespaceName() string {
	return s.Namespace
}

func (s *State) normalize() error {
	resources, providerState, err := compute.CanonicalManagedResources(s.Compute.ManagedResources, s.Compute.ProviderState)
	if err != nil {
		return err
	}
	s.Compute.ManagedResources = resources
	s.Compute.ProviderState = providerState
	if s.Compute.Namespace == "" {
		s.Compute.Namespace = s.Namespace
	}
	if s.Compute.Server == "" {
		s.Compute.Server = s.Server
	}
	return nil
}

// RemoveDurably publishes state deletion before registry authority can be removed.
func RemoveDurably(path string) error {
	return privatefile.RemoveDurably(path)
}

func Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
