package cloudinit

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
)

func TestRenderMainSSHDConfigHardeningScriptBehavior(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	script := extractMainSSHDConfigHardeningScript(t, out)

	for name, input := range map[string]string{
		"active yes":        "PermitRootLogin yes\nPasswordAuthentication no\n",
		"active relaxed":    "PermitRootLogin prohibit-password\n",
		"absent":            "PasswordAuthentication no\n",
		"commented compact": "#PermitRootLogin yes\n",
		"commented spaced":  "   #   PermitRootLogin prohibit-password\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir() + "/sshd_config"
			if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
				t.Fatal(err)
			}
			caseScript := strings.ReplaceAll(script, "/etc/ssh/sshd_config", path)
			if runtime.GOOS == "darwin" {
				caseScript = strings.Replace(caseScript, "sed -i -E ", "sed -i '' -E ", 1)
			}
			cmd := exec.Command("sh", "-c", caseScript)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("hardening script failed: %v\n%s", err, out)
			}
			gotBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			active := activePermitRootLoginLines(string(gotBytes))
			if len(active) != 1 || active[0] != "PermitRootLogin no" {
				t.Fatalf("active PermitRootLogin lines = %#v; file:\n%s", active, gotBytes)
			}
		})
	}
}

func TestRenderNormalizesMainSSHDConfigBeforeSSHRestart(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	configIndex := strings.Index(out, "sed -i -E 's/^[[:space:]]*#?[[:space:]]*PermitRootLogin")
	restartIndex := strings.Index(out, "systemctl restart ssh")
	if configIndex < 0 || restartIndex < 0 || configIndex > restartIndex {
		t.Fatalf("main sshd_config normalization should run before ssh restart\n%s", out)
	}
}

func extractMainSSHDConfigHardeningScript(t *testing.T, out string) string {
	t.Helper()
	startMarker := "    if grep -Eq '^[[:space:]]*#?[[:space:]]*PermitRootLogin[[:space:]]+' /etc/ssh/sshd_config; then"
	start := strings.Index(out, startMarker)
	if start < 0 {
		t.Fatalf("missing main sshd_config hardening script\n%s", out)
	}
	endRel := strings.Index(out[start:], "\n  - systemctl restart ssh")
	if endRel < 0 {
		t.Fatalf("missing ssh restart after main sshd_config hardening script\n%s", out)
	}
	lines := strings.Split(out[start:start+endRel], "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "    ")
	}
	return strings.Join(lines, "\n")
}

func activePermitRootLoginLines(s string) []string {
	var active []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PermitRootLogin ") {
			active = append(active, line)
		}
	}
	return active
}
