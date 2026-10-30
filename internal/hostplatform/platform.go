// Package hostplatform owns the reviewed controller, managed-host, and direct
// apt-package support baselines shared by provisioning and repair paths.
package hostplatform

import (
	"slices"
	"strings"
)

const (
	ControllerOS           = "macOS"
	ControllerVersion      = "27"
	ControllerArchitecture = "arm64"
	ManagedHostOS          = "ubuntu"
	ManagedHostVersion     = "24.04"
	ManagedHostCodename    = "noble"

	caCertificatesPackage = "ca-certificates"
	// USN-8944-1 drops the distrusted emSign ECC Root CA - G3, so older
	// bundles keep accepting chains Mozilla no longer trusts.
	caCertificatesVersion = "20260816ubuntu0.24.04.1"
	curlPackage           = "curl"
	// USN-8932-1 security fix; git below its floor regresses against it.
	curlVersion       = "8.5.0-2ubuntu10.16"
	gnupgPackage      = "gnupg"
	gnupgVersion      = "2.4.4-2ubuntu17.6"
	ufwPackage        = "ufw"
	ufwVersion        = "0.36.2-6"
	apparmorPackage   = "apparmor"
	apparmorVersion   = "4.0.1really4.0.1-0ubuntu0.24.04.8"
	unattendedPackage = "unattended-upgrades"
	unattendedVersion = "2.9.1+nmu4ubuntu1"
	jqPackage         = "jq"
	jqVersion         = "1.7.1-3ubuntu0.24.04.2"

	gitPackage = "git"
	// Repairs the LP#2171054 regression the curl security update causes, so
	// both floors must move together.
	gitVersion           = "1:2.43.0-1ubuntu7.4"
	opensshClientPackage = "openssh-client"
	// USN-8928-1 security fix.
	opensshClientVersion = "1:9.6p1-3ubuntu13.20"
	htopPackage          = "htop"
	htopVersion          = "3.3.0-4build1"
	dockerCEPackage      = "docker-ce"
	dockerCEVersion      = "5:29.9.2-1~ubuntu.24.04~noble"
	dockerCLIPackage     = "docker-ce-cli"
	dockerCLIVersion     = "5:29.9.2-1~ubuntu.24.04~noble"
	containerdPackage    = "containerd.io"
	containerdVersion    = "2.4.2-3~ubuntu.24.04~noble"
	dockerBuildxPackage  = "docker-buildx-plugin"
	dockerBuildxVersion  = "0.38.1-1~ubuntu.24.04~noble"
	dockerComposePackage = "docker-compose-plugin"
	dockerComposeVersion = "5.6.1-1~ubuntu.24.04~noble"
	cloudflaredPackage   = "cloudflared"
	// Older connectors still trust eight eMudhra intermediate CAs whose
	// private keys were compromised, so hosts below this floor fail doctor.
	cloudflaredVersion = "2026.10.2"
)

var (
	managedHostArchitectures       = []string{"amd64", "arm64"}
	managedHostKernelArchitectures = []string{"x86_64", "aarch64", "arm64"}
	managedHostImageArchitectures  = []string{"x86", "x64", "amd64", "arm", "arm64", "aarch64"}
)

// PackageBaseline is a reviewed minimum. A newer installed package remains
// valid so Ubuntu security updates are never downgraded by Serverpro.
type PackageBaseline struct {
	Name           string
	MinimumVersion string
}

var basePackageBaselines = []PackageBaseline{
	{Name: caCertificatesPackage, MinimumVersion: caCertificatesVersion},
	{Name: curlPackage, MinimumVersion: curlVersion},
	{Name: gnupgPackage, MinimumVersion: gnupgVersion},
	{Name: ufwPackage, MinimumVersion: ufwVersion},
	{Name: apparmorPackage, MinimumVersion: apparmorVersion},
	{Name: unattendedPackage, MinimumVersion: unattendedVersion},
	{Name: jqPackage, MinimumVersion: jqVersion},
}

var tailscalePrerequisitePackageBaselines = []PackageBaseline{
	{Name: caCertificatesPackage, MinimumVersion: caCertificatesVersion},
	{Name: curlPackage, MinimumVersion: curlVersion},
	{Name: jqPackage, MinimumVersion: jqVersion},
}

var gitPackageBaselines = []PackageBaseline{
	{Name: gitPackage, MinimumVersion: gitVersion},
	{Name: opensshClientPackage, MinimumVersion: opensshClientVersion},
}

var dockerPackageBaselines = []PackageBaseline{
	{Name: dockerCEPackage, MinimumVersion: dockerCEVersion},
	{Name: dockerCLIPackage, MinimumVersion: dockerCLIVersion},
	{Name: containerdPackage, MinimumVersion: containerdVersion},
	{Name: dockerBuildxPackage, MinimumVersion: dockerBuildxVersion},
	{Name: dockerComposePackage, MinimumVersion: dockerComposeVersion},
}

var htopPackageBaselines = []PackageBaseline{
	{Name: htopPackage, MinimumVersion: htopVersion},
}

func ManagedHostArchitectures() []string {
	return slices.Clone(managedHostArchitectures)
}

func ManagedHostKernelArchitectures() []string {
	return slices.Clone(managedHostKernelArchitectures)
}

func ManagedHostImageArchitectures() []string {
	return slices.Clone(managedHostImageArchitectures)
}

func BasePackageBaselines() []PackageBaseline {
	return slices.Clone(basePackageBaselines)
}

func TailscalePrerequisitePackageBaselines() []PackageBaseline {
	return slices.Clone(tailscalePrerequisitePackageBaselines)
}

func GitPackageBaselines() []PackageBaseline {
	return slices.Clone(gitPackageBaselines)
}

func DockerPackageBaselines() []PackageBaseline {
	return slices.Clone(dockerPackageBaselines)
}

func HtopPackageBaselines() []PackageBaseline {
	return slices.Clone(htopPackageBaselines)
}

func BootstrapPackageBaselines() []PackageBaseline {
	return slices.Concat(BasePackageBaselines(), GitPackageBaselines(), DockerPackageBaselines(), HtopPackageBaselines())
}

func CloudflaredPackageBaseline() PackageBaseline {
	return PackageBaseline{Name: cloudflaredPackage, MinimumVersion: cloudflaredVersion}
}

func PackageNames(packages []PackageBaseline) []string {
	names := make([]string, len(packages))
	for i, pkg := range packages {
		names[i] = pkg.Name
	}
	return names
}

func APTTokens(packages []PackageBaseline) []string {
	tokens := make([]string, len(packages))
	for i, pkg := range packages {
		tokens[i] = "apt:" + pkg.Name
	}
	return tokens
}

func PackageBaselineManifest(packages []PackageBaseline) string {
	rows := make([]string, len(packages))
	for i, pkg := range packages {
		rows[i] = pkg.Name + "|" + pkg.MinimumVersion
	}
	return strings.Join(rows, "\n")
}
