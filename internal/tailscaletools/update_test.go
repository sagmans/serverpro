package tailscaletools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/hostplatform"
)

func TestManifestPinsApprovedRelease(t *testing.T) {
	if Version != "1.102.3" {
		t.Fatalf("tailscale version = %q, want 1.102.3", Version)
	}
	if AMD64SHA256 != "36ddd9b51be57ffc2990cf76323cfa13643bfbb1b8a969f6183fa164741cdef5" {
		t.Fatalf("amd64 digest = %q", AMD64SHA256)
	}
	if ARM64SHA256 != "a0fa1b154af8c61f862a2259f559f7396d96c0225f4a863eae2333e1546bbe25" {
		t.Fatalf("arm64 digest = %q", ARM64SHA256)
	}
	if RestartGrace < time.Second {
		t.Fatalf("restart grace = %s, want a delayed restart window", RestartGrace)
	}
}

func TestCheckCommandRequiresClientDaemonAndServiceVersion(t *testing.T) {
	command := CheckCommand()
	for _, want := range []string{
		"tailscale version --json",
		"tailscale status --json",
		`1.102.3`,
		"systemctl is-active tailscaled",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("check command missing %q: %s", want, command)
		}
	}
}

func TestUpdateScriptValidatesHostBeforePackageMutation(t *testing.T) {
	script := UpdateScript()
	mainStart := strings.Index(script, "main() {")
	hostCheck := strings.Index(script[mainStart:], "require_supported_host")
	packageMutation := strings.Index(script[mainStart:], "install_prerequisites")
	if mainStart < 0 || hostCheck < 0 || packageMutation < 0 || hostCheck > packageMutation {
		t.Fatalf("host gate must precede package mutation in main")
	}
}

func TestUpdateScriptReexecutesBashThroughProductionShellDelivery(t *testing.T) {
	if _, err := exec.LookPath("dash"); err != nil {
		t.Skip("dash is required to mirror supported Ubuntu/Debian /bin/sh")
	}
	path := filepath.Join(t.TempDir(), "update.sh")
	if err := os.WriteFile(path, []byte(UpdateScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dash", path)
	cmd.Env = append(os.Environ(), "SERVERPRO_TAILSCALE_SOURCE_ONLY=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("update script through production shell delivery: %v: %s", err, out)
	}
}

func TestUpdateScriptClearsPOSIXModeBeforeBashReexec(t *testing.T) {
	if _, err := exec.LookPath("dash"); err != nil {
		t.Skip("dash is required to mirror supported Ubuntu/Debian /bin/sh")
	}
	path := filepath.Join(t.TempDir(), "update.sh")
	if err := os.WriteFile(path, []byte(UpdateScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dash", path)
	cmd.Env = append(os.Environ(), "SERVERPRO_TAILSCALE_SOURCE_ONLY=1", "POSIXLY_CORRECT=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("POSIX mode caused interpreter re-exec loop: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("update script through POSIX shell delivery: %v: %s", err, out)
	}
}

func TestUpdateScriptPinsPrerequisiteFloors(t *testing.T) {
	script := UpdateScript()
	for _, pkg := range hostplatform.TailscalePrerequisitePackageBaselines() {
		if want := pkg.Name + "|" + pkg.MinimumVersion; !strings.Contains(script, want) {
			t.Fatalf("update script missing package baseline %q", want)
		}
	}
	for _, want := range []string{
		"export LC_ALL=C",
		"SERVERPRO_TAILSCALE_HOST_OS='ubuntu'",
		"SERVERPRO_TAILSCALE_HOST_VERSION='24.04'",
		"SERVERPRO_TAILSCALE_HOST_CODENAME='noble'",
		"SERVERPRO_TAILSCALE_PACKAGES='ca-certificates curl jq'",
		`verify_package_candidates "${packages[@]}"`,
		`verify_package_minimums "${packages[@]}"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("update script missing %q", want)
		}
	}
}

func TestUpdateScriptCandidateFloorExecutableMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, installedState, installedVersion, candidate string
		wantOK                                            bool
	}{
		{name: "candidate-at-floor", candidate: hostplatform.TailscalePrerequisitePackageBaselines()[0].MinimumVersion, wantOK: true},
		{name: "config-files-newer-candidate-below-floor", installedState: "config-files", installedVersion: "3.0", candidate: "0.0.0"},
		{name: "candidate-below-floor", candidate: "0.0.0"},
		{name: "missing-candidate", candidate: "(none)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			scriptPath := filepath.Join(dir, "update.sh")
			if err := os.WriteFile(scriptPath, []byte(UpdateScript()), 0o600); err != nil {
				t.Fatal(err)
			}
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			installMarker := filepath.Join(dir, "installed")
			writeTailscaleFakeCommand(t, binDir, "dpkg-query", "if [ -f \"$INSTALL_MARKER\" ]; then state=installed; version=$CANDIDATE_VERSION; elif [ -n \"$INSTALLED_STATE\" ]; then state=$INSTALLED_STATE; version=$INSTALLED_VERSION; else exit 1; fi\ncase \"$*\" in *db:Status-Status*) printf '%s|%s' \"$state\" \"$version\" ;; *) printf '%s' \"$version\" ;; esac\n")
			writeTailscaleFakeCommand(t, binDir, "dpkg", "case \"${2:-}\" in 3.0|\"$CANDIDATE_VERSION\") [ \"${2:-}\" != 0.0.0 ] ;; *) exit 1 ;; esac\n")
			writeTailscaleFakeCommand(t, binDir, "apt-cache", "printf '  Candidate: %s\\n' \"$CANDIDATE_VERSION\"\n")
			writeTailscaleFakeCommand(t, binDir, "apt-get", "if [ \"${1:-}\" = update ]; then exit 0; fi\nprintf ran >\"$INSTALL_MARKER\"\n")
			cmd := exec.Command("bash", "-c", `source "$1"; install_prerequisites`, "test", scriptPath)
			cmd.Env = append(os.Environ(),
				"SERVERPRO_TAILSCALE_SOURCE_ONLY=1",
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"INSTALLED_STATE="+tc.installedState,
				"INSTALLED_VERSION="+tc.installedVersion,
				"CANDIDATE_VERSION="+tc.candidate,
				"INSTALL_MARKER="+installMarker,
			)
			err := cmd.Run()
			if tc.wantOK && err != nil {
				t.Fatalf("safe candidate rejected: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("unsafe candidate accepted")
			}
			_, statErr := os.Stat(installMarker)
			if (statErr == nil) != tc.wantOK {
				t.Fatalf("package install ran = %v, want %v", statErr == nil, tc.wantOK)
			}
		})
	}
}

func writeTailscaleFakeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateScriptPreflightsCandidatesBeforeInstall(t *testing.T) {
	script := UpdateScript()
	functionStart := strings.Index(script, "install_prerequisites() {")
	candidateCheck := strings.Index(script[functionStart:], `verify_package_candidates "${packages[@]}"`)
	install := strings.Index(script[functionStart:], `apt-get install -y --no-install-recommends "${packages[@]}"`)
	if functionStart < 0 || candidateCheck < 0 || install < 0 || candidateCheck > install {
		t.Fatalf("candidate floors must be checked before prerequisite installation")
	}
}

func TestUpdateScriptPinsArtifactsAndDelaysDaemonRestart(t *testing.T) {
	script := UpdateScript()
	for _, want := range []string{
		"SERVERPRO_TAILSCALE_VERSION='1.102.3'",
		AMD64SHA256,
		ARM64SHA256,
		"https://pkgs.tailscale.com/stable/",
		`apt-get install -y --no-install-recommends "${packages[@]}"`,
		"sha256sum -c",
		"--no-same-owner",
		`GODEBUG="tlsmlkem=1"`,
		"systemd-run",
		"--on-active",
		"SERVERPRO_TAILSCALE_SOURCE_ONLY",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("update script missing %q", want)
		}
	}
	for _, forbidden := range []string{"https://tailscale.com/install.sh", "| sh"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("update script contains forbidden marker %q", forbidden)
		}
	}

	path := filepath.Join(t.TempDir(), "update.sh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("update script syntax: %v: %s", err, out)
	}
}
