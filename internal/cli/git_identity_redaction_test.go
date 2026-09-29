package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
)

func TestDoctorRedactsGitHubPATResolvedAfterCredentialSnapshot(t *testing.T) {
	const secret = "fixture-pat-resolved-after-create-snapshot"
	for _, source := range []string{"prompted", "stored"} {
		t.Run(source, func(t *testing.T) {
			cfgPath := createTestConfig(t)
			t.Setenv("TMPDIR", t.TempDir())
			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			prior, err := credentials.LoadPartial(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if source == "stored" {
				if err := credentials.Update(cfg, func(current *credentials.Set) error { current.GitHubPAT = secret; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			a := &app{stdin: strings.NewReader(secret + "\n"), stdout: &stdout, stderr: &stderr}
			pat, err := a.storedOrPromptedGitHubPAT(cfg)
			if err != nil || pat != secret {
				t.Fatalf("resolve PAT: %v", err)
			}
			report := doctor.Report{Results: []doctor.Result{{Name: "credential check", Scope: "remote", Status: doctor.Fail, Evidence: pat}}}
			if err := a.writeDoctorReport(cfg, prior, report); err != nil {
				t.Fatal(err)
			}
			var summary struct {
				ReportPath string `json:"report_path"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(summary.ReportPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, output := range []string{stdout.String(), stderr.String(), string(saved)} {
				if strings.Contains(output, secret) {
					t.Fatal("late-resolved PAT leaked into diagnostic output")
				}
			}
		})
	}
}
