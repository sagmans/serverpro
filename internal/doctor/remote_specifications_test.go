package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/tunnel"
)

func TestRemoteSupportedPlatformCommandExecutableMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, release, architecture string
		wantOK                      bool
	}{
		{name: "supported-amd64", release: "ID=ubuntu\nVERSION_ID=24.04\nVERSION_CODENAME=noble\n", architecture: "x86_64", wantOK: true},
		{name: "wrong-codename", release: "ID=ubuntu\nVERSION_ID=24.04\nVERSION_CODENAME=jammy\n", architecture: "x86_64"},
		{name: "wrong-architecture", release: "ID=ubuntu\nVERSION_ID=24.04\nVERSION_CODENAME=noble\n", architecture: "riscv64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			releasePath := filepath.Join(dir, "os-release")
			if err := os.WriteFile(releasePath, []byte(tc.release), 0o600); err != nil {
				t.Fatal(err)
			}
			unamePath := filepath.Join(dir, "uname")
			if err := os.WriteFile(unamePath, []byte("#!/bin/sh\nprintf '%s\\n' \"$TEST_ARCHITECTURE\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			command := strings.ReplaceAll(remoteSupportedPlatformCommand(), "/etc/os-release", shell.Quote(releasePath))
			cmd := exec.Command("bash", "-c", command)
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TEST_ARCHITECTURE="+tc.architecture)
			err := cmd.Run()
			if tc.wantOK && err != nil {
				t.Fatalf("supported platform rejected: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("unsupported platform accepted")
			}
		})
	}
}

func TestRemoteCheckSpecificationsStartWithBlockingPlatformGate(t *testing.T) {
	cfg := config.Example("prod")
	specifications := remoteCheckSpecifications(cfg)
	if len(specifications) == 0 || !specifications[0].blocksFixesOnFailure {
		t.Fatal("supported-platform check must block every later fix")
	}
	if len(specifications[0].readCommands) != 1 || specifications[0].readCommands[0] != remoteSupportedPlatformCommand() {
		t.Fatalf("first remote check is not the supported-platform command: %+v", specifications[0])
	}
	plan := buildRemoteReadPlan(cfg)
	if len(plan.commands) == 0 || plan.commands[0].Script != remoteSupportedPlatformCommand() {
		t.Fatal("batch plan must evaluate the supported platform first")
	}
}

func TestRemoteCheckSpecificationsOwnSequentialAndBatchReadOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Example("prod")
	cfg.Network.Ingress = "cloudflare-tunnel"
	runner := &scriptedRemote{responses: map[string][]remoteCall{
		cloudInitWaitCommand: {{out: "status: done", err: errors.New("exit status 2")}},
	}}
	_ = remoteChecksSequential(context.Background(), cfg, runner, "prod-01", Options{})
	plan := buildRemoteReadPlan(cfg)
	planned := make([]string, len(plan.commands))
	for i, command := range plan.commands {
		planned[i] = command.Script
	}
	if !reflect.DeepEqual(runner.commands, planned) {
		t.Fatalf("sequential reads differ from batch authority\nsequential: %+v\nplanned:    %+v", runner.commands, planned)
	}
}

func TestRemoteCheckSpecificationsVerifyCloudflaredPackageFloor(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "cloudflare-tunnel"
	want := tunnel.CheckCommand()
	for _, specification := range remoteCheckSpecifications(cfg) {
		for _, command := range specification.readCommands {
			if command == want {
				return
			}
		}
	}
	t.Fatalf("remote checks missing cloudflared package-floor command %q", want)
}

func TestRemoteCheckSpecificationsDeclareEveryLiveFix(t *testing.T) {
	cfg := config.Example("prod")
	plan := buildRemoteReadPlan(cfg)
	for _, specification := range remoteCheckSpecifications(cfg) {
		for _, command := range specification.liveCommands {
			if !plan.allowsLive(command) {
				t.Fatalf("live fix missing from batch authority: %q", command)
			}
		}
	}
}
