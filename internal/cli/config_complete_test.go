package cli

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestCompleteComputeConfigDefaultsServerAndTunnelNames(t *testing.T) {
	cfg := config.Config{Project: "demo", Server: "web"}
	cfg.Compute.Location = "fsn1"
	cfg.Compute.Size = "cax11"
	cfg.Compute.Image = "ubuntu-24.04"
	a := &app{nonInteractive: true, stdout: io.Discard}
	if err := a.completeComputeConfig(&cfg, false); err != nil {
		t.Fatal(err)
	}
	if cfg.Compute.Name != "demo-web" {
		t.Fatalf("server name = %q", cfg.Compute.Name)
	}
	if cfg.Cloudflare.Tunnel.Name != "demo-web" {
		t.Fatalf("tunnel name = %q", cfg.Cloudflare.Tunnel.Name)
	}
}

func TestCompleteTailscaleConfigDefaultsTagsWithoutPrompt(t *testing.T) {
	cfg := config.Config{Project: "demo"}
	cfg.Access.Tailscale.Tailnet = "tailnet"
	a := &app{nonInteractive: true, stdout: io.Discard}
	if err := a.completeTailscaleConfig(&cfg, false); err != nil {
		t.Fatal(err)
	}
	want := []string{"tag:serverpro-demo"}
	if !reflect.DeepEqual(cfg.Access.Tailscale.Tags, want) {
		t.Fatalf("tags = %#v, want %#v", cfg.Access.Tailscale.Tags, want)
	}
}

func TestCompleteComputeConfigKeepsTunnelDefaultAlignedWithPromptedServerName(t *testing.T) {
	cfg := config.Config{Project: "demo", Server: "web"}
	cfg.Compute.Location = "fsn1"
	cfg.Compute.Size = "cax11"
	cfg.Compute.Image = "ubuntu-24.04"
	a := &app{stdin: strings.NewReader("custom-server\n\n\n\n"), stdout: io.Discard}
	if err := a.completeComputeConfig(&cfg, true); err != nil {
		t.Fatal(err)
	}
	if cfg.Compute.Name != "custom-server" {
		t.Fatalf("server name = %q", cfg.Compute.Name)
	}
	if cfg.Cloudflare.Tunnel.Name != "custom-server" {
		t.Fatalf("tunnel name = %q", cfg.Cloudflare.Tunnel.Name)
	}
}

func TestCompleteConfigIdentityDoesNotPromptWhenAskFalse(t *testing.T) {
	cfg := config.Config{}
	a := &app{stdin: strings.NewReader("demo\n"), stdout: io.Discard}
	if err := a.completeConfigIdentity(&cfg, false); err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "" {
		t.Fatalf("project should not be prompted, got %q", cfg.Project)
	}
	if cfg.Server != config.DefaultServer() {
		t.Fatalf("server = %q", cfg.Server)
	}
}

func TestCompleteIngressConfigOffersPublicIngressChoices(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	var seen []choice
	a := &app{stdout: io.Discard, selectChoice: func(label, def string, choices []choice) (string, bool, error) {
		if label == "public ingress" {
			seen = choices
			return "cloudflare-tunnel", true, nil
		}
		return def, false, nil
	}}
	if err := a.completeIngressConfig(&cfg, true); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0].Value != "none" || seen[1].Value != "cloudflare-tunnel" {
		t.Fatalf("ingress choices = %+v", seen)
	}
	if cfg.Network.Ingress != "cloudflare-tunnel" || !cfg.Cloudflare.Tunnel.Enabled || !cfg.Cloudflare.Tunnel.CreateConnectorOnly {
		t.Fatalf("cloudflare ingress not enabled: %+v", cfg)
	}
}

func TestCompleteNetworkConfigRejectsInvalidEgressMode(t *testing.T) {
	cfg := config.Config{}
	a := &app{stdin: strings.NewReader("invalid\n"), stdout: io.Discard}
	err := a.completeNetworkConfig(&cfg, true)
	if err == nil || !strings.Contains(err.Error(), "restricted or open") {
		t.Fatalf("expected egress mode error, got %v", err)
	}
}
