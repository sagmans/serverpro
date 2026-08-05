package config

type Config struct {
	Namespace   string      `yaml:"namespace" json:"namespace"`
	Project     string      `yaml:"-" json:"-"`
	Server      string      `yaml:"server,omitempty" json:"server,omitempty"`
	Credentials Credentials `yaml:"credentials" json:"credentials"`
	Compute     Compute     `yaml:"compute" json:"compute"`
	Admin       Admin       `yaml:"admin" json:"admin"`
	Network     Network     `yaml:"network" json:"network"`
	Access      Access      `yaml:"access" json:"access"`
	Cloudflare  Cloudflare  `yaml:"cloudflare" json:"cloudflare"`
	Hardening   Hardening   `yaml:"hardening" json:"hardening"`
}

type Credentials struct {
	Mode     string `yaml:"mode" json:"mode"`
	JSONPath string `yaml:"json_path" json:"json_path"`
}

type Compute struct {
	Name     string            `yaml:"name" json:"name"`
	Location string            `yaml:"location" json:"location"`
	Size     string            `yaml:"size" json:"size"`
	Image    string            `yaml:"image" json:"image"`
	Labels   map[string]string `yaml:"labels" json:"labels"`
}

type Admin struct {
	Username             string `yaml:"username" json:"username"`
	StoreConsolePassword bool   `yaml:"store_console_password" json:"store_console_password"`
}

type Network struct {
	Ingress string `yaml:"ingress" json:"ingress"`
	Egress  Egress `yaml:"egress" json:"egress"`
}

type Egress struct {
	Mode                        string   `yaml:"mode" json:"mode"`
	PhaseLockdownAfterBootstrap bool     `yaml:"phase_lockdown_after_bootstrap" json:"phase_lockdown_after_bootstrap"`
	Allow                       []string `yaml:"allow" json:"allow"`
}

type Access struct {
	PublicSSH    bool         `yaml:"public_ssh" json:"public_ssh"`
	EmergencySSH EmergencySSH `yaml:"emergency_ssh" json:"emergency_ssh"`
	Tailscale    Tailscale    `yaml:"tailscale" json:"tailscale"`
}

type EmergencySSH struct {
	Enabled bool     `yaml:"enabled" json:"enabled"`
	CIDRs   []string `yaml:"cidrs" json:"cidrs"`
}

type Tailscale struct {
	Enabled    bool     `yaml:"enabled" json:"enabled"`
	SSH        bool     `yaml:"ssh" json:"ssh"`
	Tailnet    string   `yaml:"tailnet" json:"tailnet"`
	Tags       []string `yaml:"tags" json:"tags"`
	RootPolicy string   `yaml:"root_policy" json:"root_policy"`
}

type Cloudflare struct {
	AccountID string       `yaml:"account_id" json:"account_id"`
	Tunnel    TunnelConfig `yaml:"tunnel" json:"tunnel"`
}

type TunnelConfig struct {
	Enabled             bool       `yaml:"enabled" json:"enabled"`
	Name                string     `yaml:"name" json:"name"`
	CreateConnectorOnly bool       `yaml:"create_connector_only" json:"create_connector_only"`
	SmokeRoute          SmokeRoute `yaml:"smoke_route" json:"smoke_route"`
}

type SmokeRoute struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Hostname string `yaml:"hostname" json:"hostname"`
	Service  string `yaml:"service" json:"service"`
}

type Hardening struct {
	Profile            string `yaml:"profile" json:"profile"`
	UnattendedUpgrades bool   `yaml:"unattended_upgrades" json:"unattended_upgrades"`
	AppArmor           bool   `yaml:"apparmor" json:"apparmor"`
	UFW                bool   `yaml:"ufw" json:"ufw"`
	JournaldPersistent bool   `yaml:"journald_persistent" json:"journald_persistent"`
}
