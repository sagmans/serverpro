package mesh

import "encoding/json"

// AuthKey is a short-lived mesh enrollment key.
type AuthKey struct {
	ID           string              `json:"id"`
	Key          string              `json:"key"`
	Description  string              `json:"description"`
	Capabilities AuthKeyCapabilities `json:"capabilities"`
}

type AuthKeyCapabilities struct {
	Devices AuthKeyDeviceCapabilities `json:"devices"`
}

type AuthKeyDeviceCapabilities struct {
	Create AuthKeyCreateCapabilities `json:"create"`
}

type AuthKeyCreateCapabilities struct {
	Tags []string `json:"tags"`
}

// Device is provider-neutral mesh device identity and health evidence.
type Device struct {
	ID                 string   `json:"id"`
	NodeID             string   `json:"nodeId"`
	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	Addresses          []string `json:"addresses"`
	Tags               []string `json:"tags"`
	Online             bool     `json:"online"`
	ConnectedToControl bool     `json:"connectedToControl"`
}

// DNSConfig mirrors the tailnet DNS posture that decides whether quad100 has a
// usable upstream for public names (2026-07 quad100 SERVFAIL incident).
type DNSConfig struct {
	MagicDNS          bool
	GlobalNameservers []string
}

type Policy struct {
	SSH []SSHRule `json:"ssh"`
}

type SSHRule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
	Users  []string `json:"users"`
	extra  map[string]json.RawMessage
}

// UnmarshalJSON retains provider fields serverpro does not model because policy
// reconciliation owns only destination changes, not the rest of an SSH rule.
func (r *SSHRule) UnmarshalJSON(data []byte) error {
	var known struct {
		Action string   `json:"action"`
		Src    []string `json:"src"`
		Dst    []string `json:"dst"`
		Users  []string `json:"users"`
	}
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"action", "src", "dst", "users"} {
		delete(fields, name)
	}
	if len(fields) == 0 {
		fields = nil
	}
	*r = SSHRule{Action: known.Action, Src: known.Src, Dst: known.Dst, Users: known.Users, extra: fields}
	return nil
}

// MarshalJSON restores retained fields while the typed fields carry approved edits.
func (r SSHRule) MarshalJSON() ([]byte, error) {
	fields := map[string]any{"action": r.Action, "src": r.Src, "dst": r.Dst, "users": r.Users}
	for name, value := range r.extra {
		fields[name] = value
	}
	return json.Marshal(fields)
}

type PolicyChange struct {
	TagOwners []string
	SSHRule   bool
}

type PolicyReconcilePlan struct {
	TagOwners []string  `json:"tag_owners"`
	SSHRules  []SSHRule `json:"ssh_rules"`
}

func (p PolicyReconcilePlan) Empty() bool {
	return len(p.TagOwners) == 0 && len(p.SSHRules) == 0
}
