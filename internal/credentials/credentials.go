package credentials

type Set struct {
	Namespace      string `json:"namespace"`
	Server         string `json:"server"`
	ServerProvider string `json:"server_provider_token"`
	Tailscale      string `json:"tailscale_token"`
	TSAuthKey      string `json:"tailscale_auth_key"`
	Cloudflare     string `json:"cloudflare_token"`
	// AdminSudoPassword and GitHubPAT are operator auth, not service tokens,
	// but share the same private-file lifetime and redaction path.
	AdminSudoPassword string `json:"admin_sudo_password,omitempty"`
	GitHubPAT          string `json:"github_pat,omitempty"`
}
