package hostplatform

import (
	"slices"
	"testing"
)

func TestSupportedRuntimeContract(t *testing.T) {
	if ControllerOS != "macOS" || ControllerVersion != "27" || ControllerArchitecture != "arm64" {
		t.Fatalf("controller support = %s %s %s", ControllerOS, ControllerVersion, ControllerArchitecture)
	}
	if ManagedHostOS != "ubuntu" || ManagedHostVersion != "24.04" || ManagedHostCodename != "noble" {
		t.Fatalf("managed host support = %s %s %s", ManagedHostOS, ManagedHostVersion, ManagedHostCodename)
	}
	if !slices.Equal(ManagedHostArchitectures(), []string{"amd64", "arm64"}) {
		t.Fatalf("managed host architectures = %v", ManagedHostArchitectures())
	}
	if !slices.Equal(ManagedHostKernelArchitectures(), []string{"x86_64", "aarch64", "arm64"}) {
		t.Fatalf("managed host kernel architectures = %v", ManagedHostKernelArchitectures())
	}
	if !slices.Equal(ManagedHostImageArchitectures(), []string{"x86", "x64", "amd64", "arm", "arm64", "aarch64"}) {
		t.Fatalf("managed host image architectures = %v", ManagedHostImageArchitectures())
	}
}

func TestReviewedPackageBaselines(t *testing.T) {
	want := map[string]string{
		"ca-certificates":       "20260601~24.04.1",
		"curl":                  "8.5.0-2ubuntu10.13",
		"gnupg":                 "2.4.4-2ubuntu17.4",
		"ufw":                   "0.36.2-6",
		"apparmor":              "4.0.1really4.0.1-0ubuntu0.24.04.7",
		"unattended-upgrades":   "2.9.1+nmu4ubuntu1",
		"jq":                    "1.7.1-3ubuntu0.24.04.2",
		"git":                   "1:2.43.0-1ubuntu7.3",
		"openssh-client":        "1:9.6p1-3ubuntu13.18",
		"htop":                  "3.3.0-4build1",
		"docker-ce":             "5:29.7.2-1~ubuntu.24.04~noble",
		"docker-ce-cli":         "5:29.7.2-1~ubuntu.24.04~noble",
		"containerd.io":         "2.3.3-1~ubuntu.24.04~noble",
		"docker-buildx-plugin":  "0.36.1-1~ubuntu.24.04~noble",
		"docker-compose-plugin": "5.5.0-1~ubuntu.24.04~noble",
		"cloudflared":           "2026.8.2",
	}
	got := make(map[string]string)
	for _, pkg := range append(BootstrapPackageBaselines(), CloudflaredPackageBaseline()) {
		if _, exists := got[pkg.Name]; exists {
			t.Fatalf("duplicate package baseline %q", pkg.Name)
		}
		got[pkg.Name] = pkg.MinimumVersion
	}
	if len(got) != len(want) {
		t.Fatalf("package baseline count = %d, want %d: %v", len(got), len(want), got)
	}
	for name, version := range want {
		if got[name] != version {
			t.Fatalf("%s baseline = %q, want %q", name, got[name], version)
		}
	}
}

func TestTailscalePrerequisitesUseReviewedBaseBaselines(t *testing.T) {
	got := TailscalePrerequisitePackageBaselines()
	want := []PackageBaseline{
		{Name: "ca-certificates", MinimumVersion: "20260601~24.04.1"},
		{Name: "curl", MinimumVersion: "8.5.0-2ubuntu10.13"},
		{Name: "jq", MinimumVersion: "1.7.1-3ubuntu0.24.04.2"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Tailscale prerequisite baselines = %v, want %v", got, want)
	}
}

func TestPackageManifestKeepsAPTNamesSeparateFromVersionFloors(t *testing.T) {
	packages := []PackageBaseline{
		{Name: "git", MinimumVersion: "1:2.43.0-1ubuntu7.3"},
		{Name: "curl", MinimumVersion: "8.5.0-2ubuntu10.13"},
	}
	if got := PackageNames(packages); !slices.Equal(got, []string{"git", "curl"}) {
		t.Fatalf("package names = %v", got)
	}
	if got := APTTokens(packages); !slices.Equal(got, []string{"apt:git", "apt:curl"}) {
		t.Fatalf("apt tokens = %v", got)
	}
	const wantManifest = "git|1:2.43.0-1ubuntu7.3\ncurl|8.5.0-2ubuntu10.13"
	if got := PackageBaselineManifest(packages); got != wantManifest {
		t.Fatalf("package manifest = %q, want %q", got, wantManifest)
	}
}

func TestPackageBaselineSlicesCannotMutateAuthority(t *testing.T) {
	first := BasePackageBaselines()
	first[0].Name = "changed"
	if BasePackageBaselines()[0].Name == "changed" {
		t.Fatal("caller mutated package baseline authority")
	}
}
