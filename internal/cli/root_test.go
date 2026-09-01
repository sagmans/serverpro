package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootHelpShowsResourceFirstSurface(t *testing.T) {
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"namespace", "server", "provider", "location", "size", "image", "tailnet", "doctor"} {
		listing := "\n  " + want
		if !strings.Contains(out.String(), listing) {
			t.Fatalf("missing %q in help:\n%s", want, out.String())
		}
	}
	for _, removed := range []string{"Secure Hetzner", "\n  account", "--account", "\n  create", "\n  bootstrap", "\n  start", "\n  stop", "\n  status", "\n  info", "\n  ssh", "\n  breakglass", "\n  destroy", "\n  init", "\n  up", "\n  down", "\n  rescue", "\n  auth", "\n  plan", "\n  provision", "\n  registry", "\n  completion", "verbose"} {
		if strings.Contains(out.String(), removed) {
			t.Fatalf("removed surface %q still shown:\n%s", removed, out.String())
		}
	}
}

func TestDefaultCommandOutputIsJSONDump(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"server", "create", "webapp", "-n", "demoapp", "-p", "hetzner", "--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04", "--dry-run", "--non-interactive"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Actions []struct {
			Step   string `json:"step"`
			Target string `json:"target"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("default output is not JSON:\n%s", out.String())
	}
	if len(payload.Actions) == 0 || payload.Actions[0].Step == "" {
		t.Fatalf("default JSON output missing actions: %+v", payload)
	}
	if strings.Contains(out.String(), "01  ") {
		t.Fatalf("default output used table text:\n%s", out.String())
	}
}

func TestProviderListDefaultsToJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"provider", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("provider list is not JSON:\n%s", out.String())
	}
	if len(rows) != 3 || rows[0].Name != "digitalocean" || rows[1].Name != "hetzner" || rows[2].Name != "vultr" {
		t.Fatalf("bad provider JSON rows: %+v", rows)
	}
}

func TestCommandErrorsDoNotDumpUsage(t *testing.T) {
	var errOut bytes.Buffer
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"server", "create", "webapp", "--project", "demo"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected command error")
	}
	if strings.Contains(errOut.String(), "Usage:") || strings.Contains(errOut.String(), "Error:") {
		t.Fatalf("error path dumped usage/noisy cobra output:\n%s", errOut.String())
	}
}

func TestTimeoutFlagRejectsInvalidDuration(t *testing.T) {
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"provider", "list", "--timeout", "not-a-duration"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid duration") {
		t.Fatalf("expected invalid duration error, got %v", err)
	}
}

func TestRootPreRunAppliesTimeoutDeadline(t *testing.T) {
	a := &app{stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard, timeout: "1s"}
	cmd := &cobra.Command{}
	if err := a.prepareRootCommand(cmd); err != nil {
		t.Fatal(err)
	}
	defer a.cleanupRootCommand()
	if _, ok := cmd.Context().Deadline(); !ok {
		t.Fatal("timeout deadline missing from command context")
	}
}

func TestRemovedRootSurfaceIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "create", args: []string{"create"}, want: "unknown command"},
		{name: "bootstrap", args: []string{"bootstrap"}, want: "unknown command"},
		{name: "start", args: []string{"start"}, want: "unknown command"},
		{name: "stop", args: []string{"stop"}, want: "unknown command"},
		{name: "status", args: []string{"status"}, want: "unknown command"},
		{name: "info", args: []string{"info"}, want: "unknown command"},
		{name: "ssh", args: []string{"ssh"}, want: "unknown command"},
		{name: "breakglass", args: []string{"breakglass"}, want: "unknown command"},
		{name: "destroy", args: []string{"destroy"}, want: "unknown command"},
		{name: "init", args: []string{"init"}, want: "unknown command"},
		{name: "auth", args: []string{"auth"}, want: "unknown command"},
		{name: "plan", args: []string{"plan"}, want: "unknown command"},
		{name: "provision", args: []string{"provision"}, want: "unknown command"},
		{name: "registry", args: []string{"registry"}, want: "unknown command"},
		{name: "completion", args: []string{"completion"}, want: "unknown command"},
		{name: "catalog", args: []string{"catalog"}, want: "unknown command"},
		{name: "ingress", args: []string{"ingress"}, want: "unknown command"},
		{name: "ingress add", args: []string{"ingress", "add", "webapp"}, want: "unknown command"},
		{name: "verbose", args: []string{"--" + "verbose"}, want: "unknown flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := New()
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q rejection for %v, got err=%v out=%q", tc.want, tc.args, err, out.String())
			}
		})
	}
}

func TestRemovedAliasesAreRejected(t *testing.T) {
	for _, alias := range []string{"up", "down", "rescue"} {
		t.Run(alias, func(t *testing.T) {
			cmd := New()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{alias})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("expected unknown command for %s, got %v", alias, err)
			}
		})
	}
}

func TestServerCreateDryRunParsesResourceFirstTarget(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"server", "create", "webapp", "-n", "demoapp", "-p", "hetzner", "--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04", "--dry-run", "--non-interactive"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "compute server") || !strings.Contains(out.String(), "size=cx23 image=ubuntu-24.04 location=fsn1") {
		t.Fatalf("resource-first dry-run did not render the create plan:\n%s", out.String())
	}
}

func TestServerCreateRequiresProviderOnly(t *testing.T) {
	createTestHome(t)
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"server", "create", "webapp", "-n", "demoapp", "--dry-run", "--non-interactive"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `--provider/-p is required for "serverpro server create"`) {
		t.Fatalf("expected provider requirement, got %v", err)
	}
}

func TestAccountSurfaceIsRemoved(t *testing.T) {
	for _, args := range [][]string{{"account", "list"}, {"server", "create", "webapp", "-n", "demoapp", "-p", "hetzner", "--account", "prod", "--dry-run"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := New()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || (!strings.Contains(err.Error(), "unknown command") && !strings.Contains(err.Error(), "unknown flag")) {
				t.Fatalf("expected removed account surface rejection, got %v", err)
			}
		})
	}
}

func TestServerCreateRejectsProjectFlag(t *testing.T) {
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"server", "create", "webapp", "--project", "demo", "--dry-run"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --project") {
		t.Fatalf("expected --project rejection, got %v", err)
	}
}

func TestPublicHelpUsesNamespaceNotProject(t *testing.T) {
	for _, args := range [][]string{{"server", "create", "--help"}, {"doctor", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			cmd := New()
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "project") || strings.Contains(out.String(), "--server") {
				t.Fatalf("provider-agnostic help leaked retired target terms:\n%s", out.String())
			}
			if args[0] == "server" && !strings.Contains(out.String(), "--namespace") {
				t.Fatalf("help missing namespace flag:\n%s", out.String())
			}
		})
	}
}

func TestScopedGlobalHelpMatchesExecutionContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		want   []string
		absent []string
	}{
		{name: "provider list", args: []string{"provider", "list", "--help"}, want: []string{"--timeout"}, absent: []string{"--dry-run", "--provider", "--namespace", "--non-interactive"}},
		{name: "server create", args: []string{"server", "create", "--help"}, want: []string{"--config", "--state", "--namespace", "--provider", "--non-interactive", "--dry-run", "--yes"}, absent: []string{"--all"}},
		{name: "server discover", args: []string{"server", "discover", "--help"}, want: []string{"--namespace", "--provider", "--non-interactive"}, absent: []string{"--dry-run", "--yes", "--all"}},
		{name: "server import", args: []string{"server", "import", "--help"}, want: []string{"--namespace", "--provider", "--all", "--non-interactive", "--dry-run", "--yes"}, absent: []string{"--config", "--state"}},
		{name: "location list", args: []string{"location", "list", "--help"}, want: []string{"--provider", "--non-interactive"}, absent: []string{"--dry-run", "--namespace"}},
		{name: "server ingress list", args: []string{"server", "ingress", "list", "--help"}, want: []string{"--state", "--namespace", "--non-interactive"}, absent: []string{"--dry-run", "--provider"}},
		{name: "tailnet reconcile", args: []string{"tailnet", "reconcile", "--help"}, want: []string{"--non-interactive", "--dry-run", "--yes", "--tailnet"}, absent: []string{"--state", "--namespace", "--provider"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := New()
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			help := out.String()
			for _, want := range tc.want {
				if !strings.Contains(help, want) {
					t.Fatalf("help missing supported flag %q:\n%s", want, help)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(help, absent) {
					t.Fatalf("help advertised unsupported flag %q:\n%s", absent, help)
				}
			}
		})
	}
}

func TestVersionFromBuildInfoUsesInstalledModuleVersion(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	if got := versionFromBuildInfo("dev", info, true); got != "v1.2.3" {
		t.Fatalf("version = %q", got)
	}
}

func TestVersionFromBuildInfoPreservesConfiguredReleaseVersion(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	if got := versionFromBuildInfo("v9.8.7", info, true); got != "v9.8.7" {
		t.Fatalf("version = %q", got)
	}
}

func TestVersionFromBuildInfoRejectsDevelopmentBuildInfo(t *testing.T) {
	for _, info := range []*debug.BuildInfo{nil, {Main: debug.Module{Version: ""}}, {Main: debug.Module{Version: "(devel)"}}} {
		if got := versionFromBuildInfo("dev", info, info != nil); got != "dev" {
			t.Fatalf("version = %q", got)
		}
	}
}

func TestRootVersionFlag(t *testing.T) {
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "serverpro version dev\n" {
		t.Fatalf("bad version output: %s", out.String())
	}
}

func TestParentCommandsRejectUnknownSubcommands(t *testing.T) {
	for _, parent := range []string{"namespace", "server", "provider", "location", "size", "image", "tailnet"} {
		t.Run(parent, func(t *testing.T) {
			cmd := New()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{parent, "bogus"})
			err := cmd.Execute()
			want := `unknown command "bogus" for "serverpro ` + parent + `"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %q, got %v", want, err)
			}
		})
	}
}

func TestUnsupportedScopedFlagsAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "provider flag with provider status", args: []string{"-p", "hetzner", "provider", "status", "vultr"}, want: `--provider is not supported by "serverpro provider status"`},
		{name: "namespace flag with provider list", args: []string{"-n", "demo", "provider", "list"}, want: `--namespace is not supported by "serverpro provider list"`},
		{name: "config flag with provider list", args: []string{"--config", "missing.yaml", "provider", "list"}, want: `--config is not supported by "serverpro provider list"`},
		{name: "state flag with provider list", args: []string{"--state", "missing.json", "provider", "list"}, want: `--state is not supported by "serverpro provider list"`},
		{name: "all flag with namespace list", args: []string{"-A", "namespace", "list"}, want: `--all is not supported by "serverpro namespace list"`},
		{name: "yes flag with location list", args: []string{"-y", "location", "list"}, want: `--yes is not supported by "serverpro location list"`},
		{name: "dry-run flag with provider list", args: []string{"--dry-run", "provider", "list"}, want: `--dry-run is not supported by "serverpro provider list"`},
		{name: "non-interactive flag with provider list", args: []string{"--non-interactive", "provider", "list"}, want: `--non-interactive is not supported by "serverpro provider list"`},
		{name: "dry-run flag with server status", args: []string{"--dry-run", "server", "status", "webapp"}, want: `--dry-run is not supported by "serverpro server status"`},
		{name: "dry-run flag with server ingress list", args: []string{"--dry-run", "server", "ingress", "list", "webapp"}, want: `--dry-run is not supported by "serverpro server ingress list"`},
		{name: "config flag with server delete", args: []string{"--config", "server.yaml", "server", "delete", "webapp"}, want: `--config is not supported by "serverpro server delete"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := New()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
