package state

import "time"

const RegistrySchemaVersion = 1

type Registry struct {
	SchemaVersion int                        `json:"schema_version"`
	Projects      map[string]RegistryProject `json:"namespaces"`
	UpdatedAt     time.Time                  `json:"updated_at"`
}

type RegistryProject struct {
	Servers map[string]RegistryEntry `json:"servers"`
}

type RegistryEntry struct {
	Project         string                `json:"namespace"`
	Server          string                `json:"server"`
	StatePath       string                `json:"state_path"`
	ConfigPath      string                `json:"config_path,omitempty"`
	CredentialsPath string                `json:"credentials_path,omitempty"`
	ResourceNames   RegistryResourceNames `json:"resource_names"`
	Labels          map[string]string     `json:"labels,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

type RegistryResourceNames struct {
	ComputeServer    string `json:"compute_server,omitempty"`
	CloudflareTunnel string `json:"cloudflare_tunnel,omitempty"`
}

func NewRegistry() Registry {
	return Registry{SchemaVersion: RegistrySchemaVersion, Projects: map[string]RegistryProject{}}
}
