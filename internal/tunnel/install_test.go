package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testCloudflareSigningFingerprint = "CC94B39C77AE7342A68B" +
	"89628A682D308D4E5E73"

func TestInstallScriptProtectsTokenFromServiceWrites(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	for _, want := range []string{"install -d -o root -g cloudflared -m 0750 /etc/cloudflared", "chown root:cloudflared \"$cloudflare_token\"", "chmod 0640 \"$cloudflare_token\"", "--token-file /etc/cloudflared/token", "systemctl is-active cloudflared"} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q\n%s", want, s)
		}
	}
	for _, rejected := range []string{"chown -R cloudflared:cloudflared", "chmod 0700 /etc/cloudflared", "chmod 0600 /etc/cloudflared/token"} {
		if strings.Contains(s, rejected) {
			t.Fatalf("service-writable token setup remains %q\n%s", rejected, s)
		}
	}
}

func TestInstallScriptAtomicallyReplacesTokenSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, tokenPath); err != nil {
		t.Fatal(err)
	}
	script := InstallScript("replacement")
	start := strings.Index(script, `cloudflare_token="$(mktemp`)
	end := strings.Index(script, `cloudflare_token=""`)
	if start < 0 || end < start {
		t.Fatalf("token replacement block missing\n%s", script)
	}
	block := script[start : end+len(`cloudflare_token=""`)]
	block = strings.ReplaceAll(block, "/etc/cloudflared", dir)
	block = strings.Replace(block, `chown root:cloudflared "$cloudflare_token"`, ":", 1)
	if out, err := exec.Command("sh", "-c", block).CombinedOutput(); err != nil {
		t.Fatalf("token replacement failed: %v\n%s\n%s", err, out, block)
	}
	victimBody, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	tokenBody, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(victimBody) != "keep" || string(tokenBody) != "replacement" || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o640 {
		t.Fatalf("unsafe replacement: victim=%q token=%q mode=%v", victimBody, tokenBody, info.Mode())
	}
}

func TestInstallScriptVerifiesCloudflareAptKeyBeforeInstall(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	for _, want := range []string{
		"https://pkg.cloudflare.com/cloudflare-main.gpg",
		testCloudflareSigningFingerprint,
		"gpg --batch --show-keys --with-colons --fingerprint",
		`awk -F: '$1 == "pub" { primary = 1; next } primary && $1 == "fpr" { print $10; primary = 0 }'`,
		"signed-by=/usr/share/keyrings/cloudflare-main.gpg",
		"chmod 0644 /etc/apt/sources.list.d/cloudflared.list",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing verified Cloudflare package setup %q\n%s", want, s)
		}
	}
	verifyAt := strings.Index(s, "key_fingerprint=")
	installAt := strings.Index(s, "install -m 0644 \"$cloudflare_key\"")
	if verifyAt < 0 || installAt < 0 || verifyAt >= installAt {
		t.Fatalf("script must verify Cloudflare package key before trusting it\n%s", s)
	}
	if strings.Contains(s, "-o /usr/share/keyrings/cloudflare-main.gpg") {
		t.Fatalf("script must not trust a downloaded key before fingerprint verification\n%s", s)
	}
}
