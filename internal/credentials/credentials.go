package credentials

type Set struct {
	Namespace      string `json:"namespace"`
	Server         string `json:"server"`
	ServerProvider string `json:"server_provider_token"`
	// ServerProviderName binds the token to the vendor it was entered for, so a
	// create with no state to compare has evidence other than its own flag. It
	// names a vendor and holds no secret, so Secrets leaves it out. Files from
	// before this field leave it empty, and create treats that as unverified.
	ServerProviderName string `json:"server_provider_name,omitempty"`
	Tailscale          string `json:"tailscale_token"`
	TSAuthKey          string `json:"tailscale_auth_key"`
	Cloudflare         string `json:"cloudflare_token"`
	// AdminSudoPassword and GitHubPAT are operator auth, not service tokens,
	// but share the same private-file lifetime and redaction path.
	AdminSudoPassword string `json:"admin_sudo_password,omitempty"`
	GitHubPAT         string `json:"github_pat,omitempty"`
}
