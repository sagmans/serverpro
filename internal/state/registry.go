package state

import "time"

const RegistrySchemaVersion = 1

type Registry struct {
	SchemaVersion int                          `json:"schema_version"`
	Namespaces    map[string]RegistryNamespace `json:"namespaces"`
	UpdatedAt     time.Time                    `json:"updated_at"`
}

type RegistryNamespace struct {
	Servers map[string]RegistryEntry `json:"servers"`
}

type RegistryEntry struct {
	Namespace       string                `json:"namespace"`
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
	return Registry{SchemaVersion: RegistrySchemaVersion, Namespaces: map[string]RegistryNamespace{}}
}
