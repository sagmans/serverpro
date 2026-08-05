package tailscale

import "testing"

func TestNewConfiguresDefaultAPI(t *testing.T) {
	client := New("token", "example.com")
	if client.tailnet != "example.com" || client.api.BaseURL != "https://api.tailscale.com/api/v2" || client.api.Token != "token" {
		t.Fatalf("bad client config: %+v", client)
	}
}
