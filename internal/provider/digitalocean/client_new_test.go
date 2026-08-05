package digitalocean

import "testing"

// WHY: New is the default production ClientFactory. Adapter behavior is covered
// via NewWithHTTP, but the hardcoded upstream base URL is only set here; pin it
// so a typo cannot silently point provisioning at the wrong DigitalOcean endpoint.

func TestNewPinsDigitalOceanAPIBaseURL(t *testing.T) {
	if got := New("token").api.BaseURL; got != "https://api.digitalocean.com/v2" {
		t.Fatalf("base URL = %q", got)
	}
	if got := New("token").api.Token; got != "token" {
		t.Fatalf("token not wired: %q", got)
	}
}
