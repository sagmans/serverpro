package config

type Config struct {
	Namespace   string      `yaml:"namespace" json:"namespace"`
	Server      string      `yaml:"server,omitempty" json:"server,omitempty"`
	Credentials Credentials `yaml:"credentials" json:"credentials"`
	Compute     Compute     `yaml:"compute" json:"compute"`
	Admin       Admin       `yaml:"admin" json:"admin"`
	Network     Network     `yaml:"network" json:"network"`
	Access      Access      `yaml:"access" json:"access"`
	Cloudflare  Cloudflare  `yaml:"cloudflare" json:"cloudflare"`
	Hardening   Hardening   `yaml:"hardening" json:"hardening"`
	// Git is optional: an omitted section keeps git/GitHub untouched on the host.
	Git Git `yaml:"git,omitempty" json:"git,omitempty"`
}

type Credentials struct {
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
	Mode                        string `yaml:"mode" json:"mode"`
	PhaseLockdownAfterBootstrap bool   `yaml:"phase_lockdown_after_bootstrap" json:"phase_lockdown_after_bootstrap"`
}

type Access struct {
	PublicSSH bool      `yaml:"public_ssh" json:"public_ssh"`
	Tailscale Tailscale `yaml:"tailscale" json:"tailscale"`
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
	Enabled             bool   `yaml:"enabled" json:"enabled"`
	Name                string `yaml:"name" json:"name"`
	CreateConnectorOnly bool   `yaml:"create_connector_only" json:"create_connector_only"`
}

type GitIdentity struct {
	Name  string `yaml:"name" json:"name"`
	Email string `yaml:"email" json:"email"`
}

type GitAccess string

const (
	GitAccessNone       GitAccess = "none"
	GitAccessDeployKey  GitAccess = "deploy-key"
	GitAccessAccountKey GitAccess = "account-key"
)

// Git carries non-secret git/GitHub setup intent; secrets (PAT, private keys)
// are deliberately absent so they can never land in config or state files.
type Git struct {
	Identity         GitIdentity `yaml:"identity" json:"identity"`
	Signing          bool        `yaml:"signing" json:"signing"`
	Access           GitAccess   `yaml:"access" json:"access"`
	DeployRepository string      `yaml:"deploy_repository,omitempty" json:"deploy_repository,omitempty"`
}

type Hardening struct {
	Profile            string `yaml:"profile" json:"profile"`
	UnattendedUpgrades bool   `yaml:"unattended_upgrades" json:"unattended_upgrades"`
	AppArmor           bool   `yaml:"apparmor" json:"apparmor"`
	UFW                bool   `yaml:"ufw" json:"ufw"`
	JournaldPersistent bool   `yaml:"journald_persistent" json:"journald_persistent"`
}
