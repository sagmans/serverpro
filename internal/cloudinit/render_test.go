package cloudinit

import (
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/shell"
	"gopkg.in/yaml.v3"
)

const (
	testAdminPasswordHash           = "$6$rounds=100000$abcdefghijklmnop$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testTailscaleSigningFingerprint = "2596A99EAAB33821893C0A79458CA832957F5868"
)

func TestRenderHardeningAndTailscale(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PermitRootLogin no", "PasswordAuthentication no", "KbdInteractiveAuthentication no", "ChallengeResponseAuthentication no", "X11Forwarding no", "AllowAgentForwarding no", "AllowTcpForwarding no", "PermitTunnel no", "PermitOpen none", "ufw default deny incoming", "tailscale up", "/root/.serverpro-tailscale-authkey", "--hostname 'prod-01'", "--ssh", "groups: sudo", "hashed_passwd: " + testAdminPasswordHash} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud-init missing %q\n%s", want, out)
		}
	}
	for _, want := range []string{"grep -Eq '^[[:space:]]*#?[[:space:]]*PermitRootLogin[[:space:]]+' /etc/ssh/sshd_config", "sed -i -E 's/^[[:space:]]*#?[[:space:]]*PermitRootLogin[[:space:]]+.*/PermitRootLogin no/' /etc/ssh/sshd_config", "printf 'PermitRootLogin no\\n' > \"$tmp\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud-init should normalize main sshd_config with %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "trap cleanup EXIT") {
		t.Fatalf("cloud-init should clean bootstrap secrets with an EXIT trap\n%s", out)
	}
	if strings.Contains(out, "&& rm -f") {
		t.Fatalf("cloud-init cleanup must not depend on tailscale success\n%s", out)
	}
	if strings.Contains(out, "[ sh, -c") {
		t.Fatalf("cloud-init should avoid fragile flow-style shell commands\n%s", out)
	}
	for _, removed := range []string{"NOPASSWD", "lock_passwd: true", "chpasswd:", "\n    passwd: "} {
		if strings.Contains(out, removed) {
			t.Fatalf("cloud-init should not render %q\n%s", removed, out)
		}
	}
	if err := yaml.Unmarshal([]byte(out), new(map[string]any)); err != nil {
		t.Fatalf("cloud-init rendered invalid YAML: %v\n%s", err, out)
	}
	for _, secret := range []string{"hetzner-secret-token", "tailscale-api-token", "cf-token"} {
		if strings.Contains(out, secret) {
			t.Fatalf("cloud-init leaked %q", secret)
		}
	}
}

func TestRenderVerifiesTailscalePackageKeyBeforeInstall(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://pkgs.tailscale.com/stable/ubuntu/noble.noarmor.gpg",
		testTailscaleSigningFingerprint,
		"gpg --batch --show-keys --with-colons --fingerprint",
		`awk -F: '$1 == "pub" { primary = 1; next } primary && $1 == "fpr" { print $10; primary = 0 }'`,
		"deb [signed-by=/usr/share/keyrings/tailscale-archive-keyring.gpg] https://pkgs.tailscale.com/stable/ubuntu noble main",
		"apt-get install -y tailscale",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud-init missing verified Tailscale package setup %q\n%s", want, out)
		}
	}
	verifyAt := strings.Index(out, "key_fingerprint=")
	installAt := strings.Index(out, "install -m 0644 \"$tailscale_key\"")
	if verifyAt < 0 || installAt < 0 || verifyAt >= installAt {
		t.Fatalf("cloud-init must verify Tailscale package key before trusting it\n%s", out)
	}
	if strings.Contains(out, "https://tailscale.com/install.sh") || strings.Contains(out, "curl -fsSL https://tailscale.com/install.sh | sh") {
		t.Fatalf("cloud-init must not execute Tailscale's network bootstrap script\n%s", out)
	}
}

func TestRenderRequiresAdminUsername(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Admin.Username = ""
	_, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err == nil || !strings.Contains(err.Error(), "admin username") {
		t.Fatalf("expected admin username error, got %v", err)
	}
}

func TestRenderRequiresAuthKey(t *testing.T) {
	cfg := config.Example("prod")
	_, err := Render(Input{Config: cfg, AdminPasswordHash: testAdminPasswordHash})
	if err == nil || !strings.Contains(err.Error(), "tailscale auth key") {
		t.Fatalf("expected auth key error, got %v", err)
	}
}

func TestRenderRequiresAdminPasswordHash(t *testing.T) {
	cfg := config.Example("prod")
	_, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff"})
	if err == nil || !strings.Contains(err.Error(), "admin password hash") {
		t.Fatalf("expected admin password hash error, got %v", err)
	}
}

func TestTemplateHelpersEscapeShellAndJSON(t *testing.T) {
	if got := shell.Quote("prod's web"); got != `'prod'\''s web'` {
		t.Fatalf("shellQuote = %s", got)
	}
	if got := jsonString(`prod"web`); got != `"prod\"web"` {
		t.Fatalf("jsonString = %s", got)
	}
}
