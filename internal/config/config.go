package config

type Config struct {
	// SchemaVersion stamps the writing build so legacy files with tool-forced
	// field values can be migrated without guessing operator intent.
	SchemaVersion int         `yaml:"schema_version,omitempty" json:"schema_version,omitempty"`
	Namespace     string      `yaml:"namespace" json:"namespace"`
	Server        string      `yaml:"server,omitempty" json:"server,omitempty"`
	Credentials   Credentials `yaml:"credentials" json:"credentials"`
	Compute       Compute     `yaml:"compute" json:"compute"`
	Admin         Admin       `yaml:"admin" json:"admin"`
	Network       Network     `yaml:"network" json:"network"`
	Access        Access      `yaml:"access" json:"access"`
	Cloudflare    Cloudflare  `yaml:"cloudflare" json:"cloudflare"`
	Hardening     Hardening   `yaml:"hardening" json:"hardening"`
	Reboot        Reboot      `yaml:"reboot" json:"reboot"`
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

// Egress modes. Restricted is the default and the only mode that provisioning
// locks down and doctor checks; open is the explicit opt-out for workloads
// that need arbitrary outbound ports.
const (
	EgressModeRestricted = "restricted"
	EgressModeOpen       = "open"
)

type Egress struct {
	Mode                        string `yaml:"mode" json:"mode"`
	PhaseLockdownAfterBootstrap bool   `yaml:"phase_lockdown_after_bootstrap" json:"phase_lockdown_after_bootstrap"`
}

// LockdownDue is the one rule for whether serverpro owes a restricted egress
// lockdown on the host. Doctor asks it before it checks or repairs the
// lockdown, and create asks it before it fails a run that has no remote host,
// so an explicit phase_lockdown_after_bootstrap: false is never undone by a
// doctor check or --fix. The egress phase of create and bootstrap reads the
// phase flag directly, because in open mode that phase also resets outgoing
// traffic to allow; for restricted mode both agree.
func (e Egress) LockdownDue() bool {
	return e.Mode == EgressModeRestricted && e.PhaseLockdownAfterBootstrap
}

// OpenSSH modes. Disabled is the default: Tailscale SSH is the only admin
// path, so a listening sshd is attack surface with no user. Enabled is the
// explicit opt-back-in for a host that needs OpenSSH, for example a provider
// console that connects through sshd.
const (
	OpenSSHDisabled = "disabled"
	OpenSSHEnabled  = "enabled"
)

type Access struct {
	PublicSSH bool      `yaml:"public_ssh" json:"public_ssh"`
	OpenSSH   string    `yaml:"openssh" json:"openssh"`
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
