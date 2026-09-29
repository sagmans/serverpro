package credentials

import (
	"reflect"
	"testing"
)

func TestMissingReportsRequiredServiceTokens(t *testing.T) {
	missing := (Set{}).Missing()
	want := []string{"server provider API token", "Tailscale API token", "Cloudflare API token"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("Missing() = %#v, want %#v", missing, want)
	}
}

func TestValidateAcceptsTailscaleAuthKey(t *testing.T) {
	// Stored operator auth is valid as long as service tokens are complete.
	if err := (Set{ServerProvider: "h", Tailscale: "ts", TSAuthKey: "auth", Cloudflare: "cf"}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretsIncludesServiceSecretValues(t *testing.T) {
	secrets := (Set{ServerProvider: "h", Tailscale: "ts", TSAuthKey: "auth", Cloudflare: "cf", AdminSudoPassword: "sudo", GitHubPAT: "pat"}).Secrets()
	want := []string{"h", "ts", "auth", "cf", "sudo", "pat"}
	if !reflect.DeepEqual(secrets, want) {
		t.Fatalf("Secrets() = %#v, want %#v", secrets, want)
	}
}
