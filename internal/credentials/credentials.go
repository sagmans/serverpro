package credentials

type Set struct {
	Namespace      string `json:"namespace"`
	Server         string `json:"server"`
	ServerProvider string `json:"server_provider_token"`
	Tailscale      string `json:"tailscale_token"`
	TSAuthKey      string `json:"tailscale_auth_key"`
	Cloudflare     string `json:"cloudflare_token"`
}
