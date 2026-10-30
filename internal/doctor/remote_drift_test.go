package doctor

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/cloudinit"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/network"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/shell"
)

// driftProbe runs a drift command under sh with fake host tools first on
// PATH, after pointing the command's absolute host paths at test files.
type driftProbe struct {
	command      string
	replacements map[string]string
	tools        map[string]string
	env          []string
}

func (p driftProbe) run(t *testing.T) (string, error) {
	t.Helper()
	bin := t.TempDir()
	for name, body := range p.tools {
		writeExecutable(t, bin, name, body)
	}
	command := p.command
	for from, to := range p.replacements {
		command = strings.ReplaceAll(command, from, to)
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append([]string{"PATH=" + bin + ":/usr/bin:/bin"}, p.env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestDriftResultFoldsProbeLinesIntoOneVerdict(t *testing.T) {
	for _, tc := range []struct {
		name, out    string
		err          error
		want         Status
		wantEvidence string
		wantFix      string
	}{
		{name: "probe error", out: "permission denied", err: errors.New("exit status 1"), want: Fail, wantEvidence: "permission denied", wantFix: driftProbeFailedRemediation},
		{name: "fail and warn", out: "warn: second\nfail: first\ndetail", want: Fail, wantEvidence: "first; second", wantFix: "lead"},
		{name: "warn only", out: "detail\nwarn: drifted\n", want: Warn, wantEvidence: "drifted", wantFix: "lead"},
		{name: "details", out: "one\n\ntwo\n", want: Pass, wantEvidence: "one; two"},
		{name: "no output", out: "\n", want: Pass, wantEvidence: "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := driftResult("probe", tc.out, tc.err, "lead")
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) || !strings.Contains(result.Remediation, tc.wantFix) {
				t.Fatalf("result = %+v, want %s with %q and remediation %q", result, tc.want, tc.wantEvidence, tc.wantFix)
			}
		})
	}
}

func TestRemoteDriftSpecificationsProbeEgressOnlyWhenRestricted(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Egress.Mode = "restricted"
	if !specificationsRead(remoteDriftSpecifications(cfg, Options{}), egressRestrictedCommand()) {
		t.Fatal("restricted egress is not probed")
	}
	// Only checks whose wanted result provisioning already writes have a
	// repair, and each repair writes what provisioning writes, not a
	// doctor-only variant.
	assertRepairs := func(repairs map[string]string) {
		t.Helper()
		for _, specification := range remoteDriftSpecifications(cfg, Options{}) {
			want := ""
			for read, repair := range repairs {
				if specificationsRead([]remoteCheckSpecification{specification}, read) {
					want = repair
				}
			}
			switch {
			case want == "" && len(specification.liveCommands) != 0:
				t.Fatalf("drift check %v declares a repair: %v", specification.readCommands, specification.liveCommands)
			case want != "" && (len(specification.liveCommands) != 1 || specification.liveCommands[0] != want):
				t.Fatalf("drift check %v repairs with %v, want the provisioning step", specification.readCommands, specification.liveCommands)
			}
		}
	}
	openSSHRepair := lifecycle.OpenSSHDisableScript(cfg.Admin.Username, nil)
	rebootRead, err := rebootSurvivabilityCommand(cfg.Reboot)
	if err != nil {
		t.Fatal(err)
	}
	rebootRepair, err := rebootgate.InstallScript(cfg.Reboot)
	if err != nil {
		t.Fatal(err)
	}
	assertRepairs(map[string]string{egressRestrictedCommand(): network.LockdownScript(cfg), openSSHDisabledCommand(): openSSHRepair, unattendedUpgradesConfigCommand(): unattendedUpgradesConfigFixCommand(), rebootRead: rebootRepair})
	cfg.Network.Egress.Mode = "open"
	if specificationsRead(remoteDriftSpecifications(cfg, Options{}), egressRestrictedCommand()) {
		t.Fatal("open egress is probed as restricted")
	}
	assertRepairs(map[string]string{openSSHDisabledCommand(): openSSHRepair, unattendedUpgradesConfigCommand(): unattendedUpgradesConfigFixCommand(), rebootRead: rebootRepair})
	// A config that opts back in to OpenSSH is neither probed nor repaired.
	cfg.Access.OpenSSH = config.OpenSSHEnabled
	if specificationsRead(remoteDriftSpecifications(cfg, Options{}), openSSHDisabledCommand()) {
		t.Fatal("enabled OpenSSH is probed as disabled")
	}
	assertRepairs(map[string]string{unattendedUpgradesConfigCommand(): unattendedUpgradesConfigFixCommand(), rebootRead: rebootRepair})
	// With the gated reboot off, doctor still runs its preflight but never
	// installs the gate.
	cfg.Reboot.Enabled = false
	assertRepairs(map[string]string{unattendedUpgradesConfigCommand(): unattendedUpgradesConfigFixCommand()})
}

func TestRemoteFixableDriftSpecificationRepairsOnlyReadableDrift(t *testing.T) {
	const (
		probe = "probe"
		fix   = "fix"
	)
	drift := remoteCall{out: "fail: drifted"}
	clean := remoteCall{out: "converged"}
	for _, tc := range []struct {
		name         string
		calls        map[string][]remoteCall
		opt          Options
		guard        func(context.Context, Options) error
		want         Status
		wantEvidence string
		wantFixRuns  int
	}{
		{name: "clean", calls: map[string][]remoteCall{probe: {clean}}, opt: Options{Fix: true}, want: Pass, wantEvidence: "converged"},
		{name: "report only", calls: map[string][]remoteCall{probe: {drift}}, want: Fail, wantEvidence: "drifted"},
		{name: "probe failed", calls: map[string][]remoteCall{probe: {{out: "denied", err: errors.New("exit status 1")}}}, opt: Options{Fix: true}, want: Fail, wantEvidence: "denied"},
		{name: "fixed", calls: map[string][]remoteCall{probe: {drift, clean}}, opt: Options{Fix: true}, want: Pass, wantEvidence: "fixed: converged", wantFixRuns: 1},
		// A repair that succeeds can still print a warning the operator must
		// act on; the OpenSSH-off repair prints them with its own prefix.
		{name: "fixed with warning", calls: map[string][]remoteCall{probe: {drift, clean}, fix: {{out: "masked\n" + lifecycle.OpenSSHWarningPrefix + "recovery path changed"}}}, opt: Options{Fix: true}, want: Warn, wantEvidence: "fixed: converged; recovery path changed", wantFixRuns: 1},
		{name: "fix failed", calls: map[string][]remoteCall{probe: {drift}, fix: {{out: "lockdown did not take effect", err: errors.New("exit status 1")}}}, opt: Options{Fix: true}, want: Fail, wantEvidence: "lockdown did not take effect", wantFixRuns: 1},
		{name: "drift remains", calls: map[string][]remoteCall{probe: {drift, drift}}, opt: Options{Fix: true}, want: Fail, wantEvidence: "fix applied but drift remains: drifted", wantFixRuns: 1},
		{name: "repair already ran", calls: map[string][]remoteCall{probe: {drift}}, opt: Options{Fix: true, fixRan: func(string) bool { return true }}, want: Fail, wantEvidence: repairAlreadyRanEvidence},
		// The OpenSSH-off guard: a server without a tunnel has no remote
		// ingress to read, while a tunnel whose ingress doctor cannot read
		// keeps the repair from running, since a route there could need sshd.
		{name: "guard without tunnel", calls: map[string][]remoteCall{probe: {drift, clean}}, opt: Options{Fix: true}, guard: tunnelSSHIngressGuard, want: Pass, wantEvidence: "fixed: converged", wantFixRuns: 1},
		{name: "guard refused", calls: map[string][]remoteCall{probe: {drift}}, opt: Options{Fix: true, tunnelID: "tun1"}, guard: tunnelSSHIngressGuard, want: Fail, wantEvidence: lifecycle.ErrTunnelIngressUnread.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &scriptedRemote{responses: tc.calls}
			results := remoteGuardedFixableDriftSpecification("drift", probe, "lead", fix, nil, tc.guard).run(context.Background(), runner, "deploy", "prod-01", tc.opt)
			fixRuns := 0
			for _, command := range runner.commands {
				if command == fix {
					fixRuns++
				}
			}
			if len(results) != 1 || results[0].Status != tc.want || !strings.Contains(results[0].Evidence, tc.wantEvidence) || fixRuns != tc.wantFixRuns {
				t.Fatalf("results = %+v fix runs = %d, want %s with %q and %d fix runs", results, fixRuns, tc.want, tc.wantEvidence, tc.wantFixRuns)
			}
		})
	}
}

func specificationsRead(specifications []remoteCheckSpecification, command string) bool {
	for _, specification := range specifications {
		for _, read := range specification.readCommands {
			if read == command {
				return true
			}
		}
	}
	return false
}

func TestRemoteDriftSpecificationReportsProbeOutput(t *testing.T) {
	command := rebootPendingCommand()
	runner := &scriptedRemote{responses: map[string][]remoteCall{command: {{out: "warn: /var/run/reboot-required present"}}}}
	results := remoteDriftSpecification(rebootPendingCheckName, command, rebootPendingRemediation("demo", "web")).run(context.Background(), runner, "deploy", "prod-01", Options{})
	if len(results) != 1 || results[0].Status != Warn || results[0].Name != rebootPendingCheckName {
		t.Fatalf("results = %+v", results)
	}
	// Doctor names the reboot but never runs one, even with --fix.
	if !strings.Contains(results[0].Remediation, "serverpro server ssh web -n demo, then "+rebootCommand) {
		t.Fatalf("remediation = %q, want the exact operator commands", results[0].Remediation)
	}
	for _, specification := range remoteDriftSpecifications(config.ExampleServer("demo", "web"), Options{}) {
		if specificationsRead([]remoteCheckSpecification{specification}, command) && len(specification.liveCommands) != 0 {
			t.Fatalf("reboot pending declares a repair: %v", specification.liveCommands)
		}
	}
}

func TestEgressRestrictedCommandProvesPolicyAndEffect(t *testing.T) {
	// The rule lines mirror ufw's own "To  Action  From" layout, v6 rows
	// included, so the anchored rule match is tested against real shapes.
	var rules strings.Builder
	for _, rule := range network.RestrictedAllowOut() {
		rules.WriteString(rule + "                         ALLOW OUT   Anywhere\n")
		rules.WriteString(rule + " (v6)                    ALLOW OUT   Anywhere (v6)\n")
	}
	denyStatus := "Status: active\nDefault: deny (incoming), deny (outgoing), deny (routed)\n\nTo                         Action      From\n--                         ------      ----\n" + rules.String()
	withoutDNS := strings.ReplaceAll(denyStatus, "\n53 ", "\n5353 ")
	for _, tc := range []struct {
		name, ufw, timeoutStatus string
		ufwStatus                string
		want                     Status
		wantEvidence             string
	}{
		{name: "locked down", ufw: denyStatus, ufwStatus: "0", timeoutStatus: "124", want: Pass, wantEvidence: "tcp 1.1.1.1:853 blocked"},
		{name: "outgoing allowed", ufw: "Status: active\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n", ufwStatus: "0", timeoutStatus: "124", want: Fail, wantEvidence: "ufw default outgoing is allow"},
		{name: "ufw inactive", ufw: "Status: inactive\n", ufwStatus: "0", timeoutStatus: "124", want: Fail, wantEvidence: "unset (ufw inactive?)"},
		{name: "allow rule missing", ufw: withoutDNS, ufwStatus: "0", timeoutStatus: "124", want: Fail, wantEvidence: "ufw allow out 53 missing"},
		{name: "probe connects", ufw: denyStatus, ufwStatus: "0", timeoutStatus: "0", want: Fail, wantEvidence: "connected; non-allowlisted egress is open"},
		{name: "ufw unreadable", ufwStatus: "1", timeoutStatus: "124", want: Fail, wantEvidence: driftProbeFailedRemediation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := driftProbe{
				command: egressRestrictedCommand(),
				tools: map[string]string{
					"ufw":     `printf '%s' "$TEST_UFW"; exit "$TEST_UFW_STATUS"`,
					"timeout": `exit "$TEST_TIMEOUT_STATUS"`,
				},
				env: []string{"TEST_UFW=" + tc.ufw, "TEST_UFW_STATUS=" + tc.ufwStatus, "TEST_TIMEOUT_STATUS=" + tc.timeoutStatus},
			}.run(t)
			result := driftResult(egressRestrictedCheckName, out, err, egressRestrictedRemediation)
			if result.Status != tc.want || !strings.Contains(result.Evidence+result.Remediation, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
		})
	}
}

func TestRebootPendingCommandReportsFlagAndNewerKernel(t *testing.T) {
	for _, tc := range []struct {
		name, flag, pkgs string
		kernels          []string
		want             Status
		wantEvidence     string
	}{
		{name: "current", kernels: []string{"6.8.0-40-generic", "6.8.0-41-generic"}, want: Pass, wantEvidence: "running kernel 6.8.0-41-generic"},
		{name: "flag with packages", flag: "*** System restart required ***\n", pkgs: "linux-base\nlibc6\nlibc6\n", kernels: []string{"6.8.0-41-generic"}, want: Warn, wantEvidence: "present for libc6 linux-base"},
		{name: "newer kernel without flag", kernels: []string{"6.8.0-41-generic", "6.8.0-100-generic"}, want: Warn, wantEvidence: "older than installed kernel 6.8.0-100-generic"},
		{name: "no kernel images", want: Pass, wantEvidence: "running kernel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			flagPath := filepath.Join(dir, "reboot-required")
			if tc.flag != "" {
				writeFile(t, flagPath, tc.flag)
			}
			if tc.pkgs != "" {
				writeFile(t, flagPath+".pkgs", tc.pkgs)
			}
			imagePrefix := filepath.Join(dir, "vmlinuz-")
			for _, kernel := range tc.kernels {
				writeFile(t, imagePrefix+kernel, "")
			}
			out, err := driftProbe{
				command:      rebootPendingCommand(),
				replacements: map[string]string{rebootRequiredPath: flagPath, kernelImagePrefix: imagePrefix},
				tools:        map[string]string{"uname": `printf '6.8.0-41-generic\n'`},
			}.run(t)
			result := driftResult(rebootPendingCheckName, out, err, rebootPendingRemediation("demo", "web"))
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
		})
	}
}

// fakeGetent answers from files under $FAKE_ETC like the libc databases do,
// exiting 2 for a missing key as getent does, so the scan can tell a missing
// group from a failed read.
const fakeGetent = `case "$1:$#" in
passwd:1) cat "$FAKE_ETC/passwd" ;;
*) grep "^$2:" "$FAKE_ETC/$1" || exit 2 ;;
esac`

func fakeEtc(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		writeFile(t, filepath.Join(dir, name), body)
	}
	return dir
}

// privilegedProbe builds the read-only probe's output the way a host prints
// it, so the repair flow below runs through the real grader: the cvtsudoers
// JSON policy, each listed command's host verdict, the verdict on the
// fixtures' secure_path, the admin uid, then the account findings.
func privilegedProbe(policy, verdicts, accounts string) string {
	return strings.Join([]string{sudoersSectionMarker, policy, sudoCommandsSectionMarker, verdicts, sudoersPathsSectionMarker, "directory\t/usr/bin\tok", adminUIDSectionMarker, "1000", accountsSectionMarker, accounts}, "\n")
}

// Sudoers fixtures for the repair test: a CI deploy user limited to a
// root-owned script is report-only and must stay a warning after the image
// account removal; an unlimited rule, an env_keep of LD_PRELOAD, and setenv
// are unknown drift that the repair must leave alone.
const (
	ciListedPolicy  = `{"Defaults": [{"Options": [{"secure_path": "/usr/bin"}]}], "User_Specs": [{"User_List": [{"username": "ci"}], "Cmnd_Specs": [{"Options": [{"authenticate": false}], "Commands": [{"command": "/usr/local/bin/deploy-app restart"}]}]}]}`
	ciListedVerdict = "/usr/local/bin/deploy-app\tok"
	unknownPolicy   = `{"Defaults": [{"Options": [{"operation": "list_add", "env_keep": ["LD_PRELOAD"]}, {"setenv": true}]}], "User_Specs": [{"User_List": [{"username": "ci"}], "Cmnd_Specs": [{"Commands": [{"command": "ALL"}]}]}]}`
	emptyPolicy     = `{"Defaults": [{"Options": [{"secure_path": "/usr/bin"}]}], "User_Specs": []}`
)

func TestPrivilegedAccountsSpecificationRepairsOnlyImageAccounts(t *testing.T) {
	if specification := remotePrivilegedAccountsSpecification("deploy", nil); len(specification.liveCommands) != 0 {
		t.Fatal("a provider without image accounts got a repair")
	}
	if specification := remotePrivilegedAccountsSpecification("deploy", []string{"deploy"}); len(specification.liveCommands) != 0 {
		t.Fatal("a declaration naming the admin user got a repair")
	}
	specification := remotePrivilegedAccountsSpecification("deploy", []string{"linuxuser"})
	if len(specification.liveCommands) != 1 || !strings.Contains(specification.liveCommands[0], "userdel -r") {
		t.Fatalf("live commands = %v, want the image account removal", specification.liveCommands)
	}
	command, removal := specification.readCommands[0], specification.liveCommands[0]
	for _, tc := range []struct {
		name, finding, after string
		wantFix              bool
		want                 Status
	}{
		{name: "unknown account stays report-only", finding: privilegedProbe(unknownPolicy, "", "fail: linuxuser2 is in privileged group sudo"), want: Fail},
		{name: "image account is removed", finding: privilegedProbe(emptyPolicy, "", "fail: provider image account linuxuser exists"), after: privilegedProbe(emptyPolicy, "", "checked groups: sudo"), wantFix: true, want: Pass},
		{name: "listed-command rule left after the removal keeps its warning", finding: privilegedProbe(ciListedPolicy, ciListedVerdict, "fail: provider image account linuxuser exists"), after: privilegedProbe(ciListedPolicy, ciListedVerdict, ""), wantFix: true, want: Warn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &scriptedRemote{responses: map[string][]remoteCall{command: {{out: tc.finding}, {out: tc.after}}}}
			results := specification.run(context.Background(), runner, "deploy", "prod-01", Options{Fix: true})
			ran := false
			for _, script := range runner.commands {
				ran = ran || script == removal
			}
			if ran != tc.wantFix || results[0].Status != tc.want {
				t.Fatalf("removal ran = %v, results = %+v", ran, results)
			}
			if tc.want == Warn && !strings.HasPrefix(results[0].Evidence, repairPartialEvidence) {
				t.Fatalf("evidence = %q, want the partial repair prefix", results[0].Evidence)
			}
		})
	}
}

// TestMetadataUserDataCommandNeverPrintsSecrets drives every branch with a
// hash in the body and in shadow, then requires neither value in the output.
func TestMetadataUserDataCommandNeverPrintsSecrets(t *testing.T) {
	const (
		path        = "/user-data"
		currentHash = "$6$current$abcdefghijklmnopqrstuv"
		staleHash   = "$y$j9T$stale$abcdefghijklmnopqrstuv"
		authKey     = "tskey-auth-secret"
	)
	for _, tc := range []struct {
		name, body, code string
		want             Status
		wantEvidence     string
	}{
		{name: "no user-data", code: "404", want: Pass, wantEvidence: metadataServiceURL + path + " has no user-data"},
		{name: "metadata unreachable", code: "000", want: Warn, wantEvidence: "answered HTTP 000"},
		{name: "clean user-data", body: "#cloud-config\nruncmd:\n  - echo " + authKey + "\n", code: "200", want: Pass, wantEvidence: "holds no password hash"},
		{name: "current hash", body: "#cloud-config\nusers:\n  - name: deploy\n    hashed_passwd: " + currentHash + "\n", code: "200", want: Fail, wantEvidence: "holds the current admin password hash"},
		{name: "stale hash", body: "#cloud-config\nchpasswd:\n  list: |\n    deploy:" + staleHash + "\n", code: "200", want: Warn, wantEvidence: "not the current admin hash"},
		{name: "plain passwd key", body: "#cloud-config\nusers:\n  - name: deploy\n    passwd: hunter2\n", code: "200", want: Warn, wantEvidence: "passwd key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			etc := fakeEtc(t, map[string]string{"shadow": "deploy:" + currentHash + ":20000:0:99999:7:::\n"})
			out, err := driftProbe{
				command: metadataUserDataCommand("deploy", path),
				tools: map[string]string{
					"curl":   `for arg; do url=$arg; done; [ "$url" = "$TEST_URL" ] || exit 7; printf '%s\n%s' "$TEST_BODY" "$TEST_CODE"`,
					"getent": fakeGetent,
				},
				env: []string{"TEST_URL=" + metadataServiceURL + path, "TEST_BODY=" + tc.body, "TEST_CODE=" + tc.code, "FAKE_ETC=" + etc},
			}.run(t)
			result := driftResult(metadataUserDataCheckName, out, err, metadataUserDataRemediation)
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
			for _, secret := range []string{currentHash, staleHash, authKey, "hunter2"} {
				if strings.Contains(out, secret) {
					t.Fatalf("probe output leaks %q: %q", secret, out)
				}
			}
		})
	}
}

// TestRemoteMetadataUserDataSpecificationSkipsWithoutProviderPath keeps an
// unprobed endpoint visible: without a provider path the check must skip,
// never pass, and must not plan a read.
func TestRemoteMetadataUserDataSpecificationSkipsWithoutProviderPath(t *testing.T) {
	specification := remoteMetadataUserDataSpecification("deploy", "")
	results := specification.run(context.Background(), &scriptedRemote{}, "deploy", "prod-01", Options{})
	if len(specification.readCommands) != 0 || len(results) != 1 || results[0].Status != Skip || results[0].Evidence != metadataUserDataUnchecked {
		t.Fatalf("specification reads=%v results=%+v", specification.readCommands, results)
	}
	cfg := config.Example("prod")
	if !specificationsRead(remoteDriftSpecifications(cfg, Options{MetadataUserDataPath: "/user-data"}), metadataUserDataCommand(cfg.Admin.Username, "/user-data")) {
		t.Fatal("provider metadata path is not probed")
	}
}

func TestTailscaleOperatorCommandPrintsOnlyOperator(t *testing.T) {
	for _, tc := range []struct {
		name, prefs, status string
		want                Status
		wantEvidence        string
	}{
		{name: "unset", prefs: "{\n  \"OperatorUser\": \"\",\n  \"Persist\": {\"PrivateNodeKey\": \"privkey:secret\"}\n}\n", status: "0", want: Pass, wantEvidence: "tailscale operator unset"},
		{name: "set", prefs: "{\n  \"OperatorUser\": \"linuxuser\",\n  \"Persist\": {\"PrivateNodeKey\": \"privkey:secret\"}\n}\n", status: "0", want: Fail, wantEvidence: "tailscale operator is linuxuser"},
		{name: "prefs unreadable", prefs: "{\"Persist\": {\"PrivateNodeKey\": \"privkey:secret\"}}", status: "1", want: Fail, wantEvidence: "tailscale debug prefs failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := driftProbe{
				command: tailscaleOperatorCommand(),
				tools:   map[string]string{"tailscale": `printf '%s' "$TEST_PREFS"; exit "$TEST_STATUS"`},
				env:     []string{"TEST_PREFS=" + tc.prefs, "TEST_STATUS=" + tc.status},
			}.run(t)
			result := driftResult(tailscaleOperatorCheckName, out, err, tailscaleOperatorRemediation)
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
			if strings.Contains(out, "privkey") {
				t.Fatalf("probe output leaks node prefs: %q", out)
			}
		})
	}
}

func TestUnattendedUpgradesConfigCommandReadsEffectiveValues(t *testing.T) {
	first := cloudinit.UnattendedUpgradesSettings()[0]
	want := cloudinit.AptBool(first.Value)
	for _, tc := range []struct {
		name, value  string
		want         Status
		wantEvidence string
	}{
		{name: "set", value: "value='" + want + "'", want: Pass, wantEvidence: first.Key + " is " + want},
		{name: "drop-in gone", value: "", want: Warn, wantEvidence: "is not set"},
		{name: "overridden by a later file", value: "value='" + cloudinit.AptBool(!first.Value) + "'", want: Fail, wantEvidence: first.Key + " is not " + want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := driftProbe{
				command: unattendedUpgradesConfigCommand(),
				tools:   map[string]string{"apt-config": unattendedAptConfigFake()},
				env:     []string{"FAKE_KEY=" + first.Key, "FAKE_VALUE=" + tc.value},
			}.run(t)
			result := driftResult(unattendedUpgradesConfigCheckName, out, err, unattendedUpgradesConfigRemediation)
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
		})
	}
}

// unattendedAptConfigFake answers the first setting from the test case and
// every other setting with its wanted value, so each case varies one option.
func unattendedAptConfigFake() string {
	fake := "case \"$1 $2 $3\" in\n  \"shell value $FAKE_KEY/b\") [ -z \"$FAKE_VALUE\" ] || printf '%s\\n' \"$FAKE_VALUE\" ;;\n"
	for _, other := range cloudinit.UnattendedUpgradesSettings()[1:] {
		fake += "  " + shell.Quote("shell value "+other.Key+"/b") + ") printf '%s\\n' " + shell.Quote("value='"+cloudinit.AptBool(other.Value)+"'") + " ;;\n"
	}
	return fake + "  *) exit 2 ;;\nesac"
}

func TestSysctlRuntimeCommandComparesFileWithKernel(t *testing.T) {
	var file, runtime strings.Builder
	for _, setting := range cloudinit.SysctlSettings() {
		file.WriteString(setting.Key + " = " + setting.Value + "\n")
		runtime.WriteString(setting.Key + "=" + setting.Value + "\n")
	}
	first := cloudinit.SysctlSettings()[0]
	for _, tc := range []struct {
		name, file, runtime string
		missing             bool
		want                Status
		wantEvidence        string
	}{
		{name: "in sync", file: "# serverpro\n" + file.String(), runtime: runtime.String(), want: Pass, wantEvidence: "compared"},
		{name: "runtime drift", file: file.String(), runtime: strings.Replace(runtime.String(), first.Key+"="+first.Value, first.Key+"=0", 1), want: Warn, wantEvidence: first.Key + " is 0 at runtime"},
		{name: "unreadable key", file: file.String() + "net.example.extra = 1\n", runtime: runtime.String(), want: Warn, wantEvidence: "net.example.extra is unreadable"},
		{name: "setting removed from file", file: strings.Replace(file.String(), first.Key+" = "+first.Value+"\n", "", 1), runtime: runtime.String(), want: Warn, wantEvidence: "lacks serverpro setting " + first.Key},
		{name: "file missing", missing: true, want: Warn, wantEvidence: "is missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			confPath := filepath.Join(dir, "99-serverpro.conf")
			if !tc.missing {
				writeFile(t, confPath, tc.file)
			}
			runtimePath := filepath.Join(dir, "runtime")
			writeFile(t, runtimePath, tc.runtime)
			out, err := driftProbe{
				command:      sysctlRuntimeCommand(),
				replacements: map[string]string{cloudinit.SysctlConfigPath: confPath},
				tools:        map[string]string{"sysctl": `grep "^$2=" "$FAKE_SYSCTL" | cut -d= -f2-`},
				env:          []string{"FAKE_SYSCTL=" + runtimePath},
			}.run(t)
			result := driftResult(sysctlRuntimeCheckName, out, err, sysctlRuntimeRemediation)
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
		})
	}
}

const (
	serveListeners = "udp   UNCONN 0      0          127.0.0.53%lo:53         0.0.0.0:*\n" +
		"tcp   LISTEN 0      4096       127.0.0.1:3000       0.0.0.0:*\n" +
		"tcp   LISTEN 0      4096       [::]:8080            [::]:*\n" +
		"tcp   LISTEN 0      4096       100.64.0.1:9000      0.0.0.0:*\n"
)

func TestTailscaleServeResult(t *testing.T) {
	for _, tc := range []struct {
		name, serve, listeners string
		want                   Status
		wantEvidence           string
	}{
		{name: "idle host", serve: "{}", want: Pass, wantEvidence: "no serve entries"},
		{name: "empty output", serve: "", want: Pass, wantEvidence: "no serve entries"},
		{name: "backends listening", serve: `{"TCP":{"443":{"HTTPS":true}},"Web":{"prod-01.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"},"/api":{"Proxy":"localhost:8080"},"/static":{"Path":"/srv/www"}}}},"Services":{"svc:app":{"TCP":{"8443":{"TCPForward":"127.0.0.1:3000"}}}}}`, listeners: serveListeners, want: Pass, wantEvidence: "3 local serve backends listening"},
		{name: "stale backend", serve: `{"Web":{"prod-01.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:4000"}}}}}`, listeners: serveListeners, want: Warn, wantEvidence: "nothing listens on port 4000"},
		{name: "tailnet-only bind does not answer loopback", serve: `{"Foreground":{"session":{"TCP":{"9000":{"TCPForward":"127.0.0.1:9000"}}}}}`, listeners: serveListeners, want: Warn, wantEvidence: "tcp 9000 forwards to 127.0.0.1:9000"},
		{name: "udp listener is not a backend", serve: `{"TCP":{"53":{"TCPForward":"127.0.0.53:53"}}}`, listeners: serveListeners, want: Warn, wantEvidence: "nothing listens on port 53"},
		{name: "remote backend skipped", serve: `{"Web":{"prod-01.ts.net:443":{"Handlers":{"/":{"Proxy":"http://10.0.0.5:3000"}}}},"TCP":{"22":{"TCPForward":"[fd7a::1]:22"}}}`, want: Pass, wantEvidence: "no serve entries"},
		{name: "funnel", serve: `{"AllowFunnel":{"prod-01.ts.net:443":true,"prod-01.ts.net:8443":false}}`, want: Fail, wantEvidence: "Funnel publishes prod-01.ts.net:443"},
		{name: "invalid json", serve: "{", want: Fail, wantEvidence: "cannot parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := tailscaleServeSectionMarker + "\n" + tc.serve + "\n" + listenersSectionMarker + "\n" + tc.listeners
			result := tailscaleServeResult(out)
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
		})
	}
	if result := tailscaleServeResult("ok"); result.Status != Fail || !strings.Contains(result.Evidence, "unexpected serve probe output") {
		t.Fatalf("output without markers = %+v, want fail", result)
	}
	if result := tailscaleServeResult(TailscaleServeIdleOutput()); result.Status != Pass {
		t.Fatalf("idle fixture = %+v, want pass", result)
	}
}

// TestTailscaleServeCommandFeedsServeResult runs the probe against fake
// tailscale and ss so the markers and listener columns stay parseable.
func TestTailscaleServeCommandFeedsServeResult(t *testing.T) {
	out, err := driftProbe{
		command: TailscaleServeCommand(),
		tools: map[string]string{
			"tailscale": `printf '%s\n' '{"TCP":{"443":{"TCPForward":"127.0.0.1:3000"}}}'`,
			"ss":        `printf '%s' "$TEST_SS"`,
		},
		env: []string{"TEST_SS=" + serveListeners},
	}.run(t)
	if err != nil {
		t.Fatalf("serve probe failed: %v\n%s", err, out)
	}
	if result := tailscaleServeResult(out); result.Status != Pass || !strings.Contains(result.Evidence, "1 local serve backends listening") {
		t.Fatalf("result = %+v from %q", result, out)
	}

	runner := &scriptedRemote{responses: map[string][]remoteCall{TailscaleServeCommand(): {{out: "tailscaled not running", err: errors.New("exit status 1")}}}}
	results := remoteTailscaleServeSpecification().run(context.Background(), runner, "deploy", "prod-01", Options{})
	if len(results) != 1 || results[0].Status != Fail || results[0].Remediation != driftProbeFailedRemediation {
		t.Fatalf("probe error results = %+v", results)
	}

}
