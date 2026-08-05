package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptCleansTokenFile(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	for _, want := range []string{"/etc/cloudflared/token", "--token-file /etc/cloudflared/token", "chmod 0600 /etc/cloudflared/token", "systemctl is-active cloudflared"} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q\n%s", want, s)
		}
	}
}

func TestInstallScriptVerifiesCloudflareAptKeyBeforeTrust(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	for _, want := range []string{cloudflareAPTKeyFingerprint, "mktemp", "gpg --show-keys --with-colons", "install -m 0644", "cloudflare_key_publish_tmp", "mv -f", "signed-by=/usr/share/keyrings/cloudflare-main.gpg", "chmod 0644 /etc/apt/sources.list.d/cloudflared.list"} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "-o /usr/share/keyrings/cloudflare-main.gpg") {
		t.Fatalf("script must not download directly into the trusted keyring\n%s", s)
	}
}

func TestInstallScriptRejectsMismatchedCloudflareAptKey(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	// Refuse to source older script shapes because their top-level privileged
	// commands would run during this executable regression test.
	if !strings.Contains(s, "SERVERPRO_TUNNEL_SOURCE_ONLY") {
		t.Fatalf("script needs source-only guard before executable helper testing\n%s", s)
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakeCommand(t, binDir, "curl", "for arg; do output=$arg; done\nprintf 'untrusted' > \"$output\"\n")
	writeFakeCommand(t, binDir, "gpg", "printf 'fpr:::::::::0000000000000000000000000000000000000000:\\n'\n")
	installMarker := filepath.Join(dir, "install-ran")
	writeFakeCommand(t, binDir, "install", "printf ran > \"$INSTALL_MARKER\"\n")

	cmd := exec.Command("bash", "-c", `source "$1"; install_cloudflare_apt_key "$2"`, "test", scriptPath, filepath.Join(dir, "trusted.gpg"))
	cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"), "INSTALL_MARKER="+installMarker)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "fingerprint mismatch") {
		t.Fatalf("mismatched key should fail closed: err=%v output=%s", err, out)
	}
	if _, err := os.Stat(installMarker); !os.IsNotExist(err) {
		t.Fatalf("mismatched key reached trusted install: %v", err)
	}
}

func TestInstallScriptAcceptsApprovedCloudflarePrimaryKeyWithSubkey(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakeCommand(t, binDir, "curl", "for arg; do output=$arg; done\nprintf 'approved' > \"$output\"\n")
	writeFakeCommand(t, binDir, "gpg", "printf 'pub:::::::::\\nfpr:::::::::"+cloudflareAPTKeyFingerprint+":\\nsub:::::::::\\nfpr:::::::::0000000000000000000000000000000000000000:\\n'\n")
	destination := filepath.Join(dir, "trusted.gpg")
	cmd := exec.Command("bash", "-c", `source "$1"; install_cloudflare_apt_key "$2"`, "test", scriptPath, destination)
	cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("approved primary key rejected: %v output=%s", err, out)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("approved key was not published: %v", err)
	}
}

func TestInstallScriptRejectsAdditionalCloudflarePrimaryKey(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakeCommand(t, binDir, "curl", "for arg; do output=$arg; done\nprintf 'untrusted' > \"$output\"\n")
	writeFakeCommand(t, binDir, "gpg", "printf 'pub:::::::::\\nfpr:::::::::"+cloudflareAPTKeyFingerprint+":\\npub:::::::::\\nfpr:::::::::0000000000000000000000000000000000000000:\\n'\n")
	installMarker := filepath.Join(dir, "install-ran")
	writeFakeCommand(t, binDir, "install", "printf ran > \"$INSTALL_MARKER\"\n")

	cmd := exec.Command("bash", "-c", `source "$1"; install_cloudflare_apt_key "$2"`, "test", scriptPath, filepath.Join(dir, "trusted.gpg"))
	cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"), "INSTALL_MARKER="+installMarker)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "fingerprint mismatch") {
		t.Fatalf("additional primary key should fail closed: err=%v output=%s", err, out)
	}
	if _, err := os.Stat(installMarker); !os.IsNotExist(err) {
		t.Fatalf("additional primary key reached trusted install: %v", err)
	}
}

func writeFakeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}
