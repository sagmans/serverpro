package rebootgate

import (
	"io/fs"
	"os/exec"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

// The host behaviour of the script (gaps, reboot, post-boot check) is proven
// by the systemd container dogfood in TESTING.md; these tests pin what
// config renders into the files doctor compares by digest.
func TestFilesRenderConfigIntoTheInstalledUnits(t *testing.T) {
	r := config.Reboot{Enabled: true, Window: "03:07", VerifyTimeout: "2m"}
	files, err := Files(r)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		mode     fs.FileMode
		contains []string
	}{
		ScriptPath:                           {0o755, []string{"#!/bin/sh\n", "VERIFY_TIMEOUT_SECONDS='120'\n", "GAP_PREFIX='" + InstalledGapPrefix + "'\n"}},
		"/etc/systemd/system/" + ServiceUnit: {0o644, []string{"ConditionPathExists=" + RebootRequiredPath + "\n", "ExecStart=" + ScriptPath + " window\n"}},
		"/etc/systemd/system/" + TimerUnit:   {0o644, []string{"OnCalendar=*-*-* 03:07:00 UTC\n", "Persistent=false\n"}},
		// 2m wait + 10m restart budget + 1m settle + 1m grace.
		"/etc/systemd/system/" + VerifyUnit: {0o644, []string{"After=multi-user.target docker.service\n", "RuntimeMaxSec=840\n", "ConditionPathExists=" + SnapshotPath + "\n"}},
	}
	if len(files) != len(want) {
		t.Fatalf("files = %d, want %d", len(files), len(want))
	}
	digests := map[string]bool{}
	for _, file := range files {
		expected, ok := want[file.Path]
		if !ok {
			t.Fatalf("unexpected file %s", file.Path)
		}
		if file.Mode != expected.mode {
			t.Fatalf("%s mode = %v, want %v", file.Path, file.Mode, expected.mode)
		}
		for _, line := range expected.contains {
			if !strings.Contains(file.Content, line) {
				t.Fatalf("%s missing %q\n%s", file.Path, line, file.Content)
			}
		}
		digests[file.SHA256()] = true
	}
	if len(digests) != len(files) {
		t.Fatal("two files share a digest, so doctor could not tell their drift apart")
	}
}

func TestRenderedScriptsParseUnderSh(t *testing.T) {
	r := config.Reboot{Enabled: true, Window: "02:00", VerifyTimeout: "10m"}
	install, err := InstallScript(r)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := PreflightProbe(r, WarningPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(probe, "(\nset -- preflight\n") || !strings.Contains(probe, "GAP_PREFIX='"+WarningPrefix+"'\n") {
		t.Fatalf("preflight probe must run the preflight in a subshell with the caller's prefix\n%s", probe[:200])
	}
	for name, script := range map[string]string{"install": install, "preflight": probe} {
		if out, err := exec.Command("sh", "-n", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("%s script does not parse: %v\n%s", name, err, out)
		}
	}
}

func TestRenderRejectsAnInvalidReboot(t *testing.T) {
	for _, r := range []config.Reboot{
		{Window: "25:00", VerifyTimeout: "10m"},
		{Window: "02:00", VerifyTimeout: "10s"},
	} {
		if _, err := Files(r); err == nil {
			t.Fatalf("Files(%+v) accepted an invalid reboot", r)
		}
		if _, err := InstallScript(r); err == nil {
			t.Fatalf("InstallScript(%+v) accepted an invalid reboot", r)
		}
	}
	if _, err := PreflightProbe(config.Reboot{Window: "02:00", VerifyTimeout: "2h"}, WarningPrefix); err == nil {
		t.Fatal("PreflightProbe accepted a verify timeout above the bound")
	}
}
