package credentials

import (
	"reflect"
	"strings"
	"testing"
)

func TestMissingReportsRequiredServiceTokens(t *testing.T) {
	missing := (Set{}).Missing()
	want := []string{"server provider API token", "Tailscale API token", "Cloudflare API token"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("Missing() = %#v, want %#v", missing, want)
	}
}

func TestValidateRejectsTailscaleAuthKey(t *testing.T) {
	err := (Set{ServerProvider: "h", Tailscale: "ts", TSAuthKey: "auth", Cloudflare: "cf"}).Validate()
	if err == nil || !strings.Contains(err.Error(), "tailscale_auth_key") {
		t.Fatalf("expected auth key error, got %v", err)
	}
}

func TestSecretsIncludesServiceSecretValues(t *testing.T) {
	secrets := (Set{ServerProvider: "h", Tailscale: "ts", TSAuthKey: "auth", Cloudflare: "cf"}).Secrets()
	want := []string{"h", "ts", "auth", "cf"}
	if !reflect.DeepEqual(secrets, want) {
		t.Fatalf("Secrets() = %#v, want %#v", secrets, want)
	}
}
