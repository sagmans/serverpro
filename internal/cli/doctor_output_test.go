package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
)

const outputSecret = "doctor-output-secret-value"

func TestDoctorOutputSummaryAndFull(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "summary", true: "full"}[full], func(t *testing.T) {
			temp := t.TempDir()
			t.Setenv("TMPDIR", temp)
			var stdout, stderr bytes.Buffer
			a := &app{stdout: &stdout, stderr: &stderr, doctorFull: full, runtimeSecrets: []string{outputSecret}}
			report := doctor.Report{Inventory: []doctor.InventoryItem{{Name: "inventory", Value: outputSecret}}, Results: []doctor.Result{{Name: "ok", Status: doctor.Pass}, {Name: "bad", Status: doctor.Fail, Evidence: outputSecret}}}
			if err := a.writeDoctorReport(config.Config{Namespace: "test", Server: "server"}, credentials.Set{}, report); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stdout.String(), outputSecret) || stderr.Len() != 0 {
				t.Fatalf("unsafe output: %s %s", &stdout, &stderr)
			}
			if full {
				paths, err := filepath.Glob(filepath.Join(temp, "serverpro-*", "doctor", "test", "server", "doctor-*.json"))
				if err != nil || len(paths) != 1 {
					t.Fatalf("full mode did not persist report: %v %v", paths, err)
				}
				data, err := os.ReadFile(paths[0])
				if err != nil || !bytes.Equal(data, stdout.Bytes()) {
					t.Fatalf("full output and saved report differ: %v", err)
				}
				var got doctor.Report
				if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || len(got.Results) != 2 || len(got.Inventory) != 1 {
					t.Fatalf("full report: %s", &stdout)
				}
				return
			}
			var got doctor.Summary
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != doctor.Fail || got.Counts.Pass != 1 || len(got.Results) != 1 {
				t.Fatalf("summary=%+v", got)
			}
			data, err := os.ReadFile(got.ReportPath)
			if err != nil {
				t.Fatal(err)
			}
			var saved doctor.Report
			if err := json.Unmarshal(data, &saved); err != nil || len(saved.Results) != 2 || len(saved.Inventory) != 1 || strings.Contains(string(data), outputSecret) {
				t.Fatalf("saved report: %s", data)
			}
		})
	}
}

func TestDoctorOutputFallbackAndWriterErrors(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	report := doctor.Report{Results: []doctor.Result{{Name: "ok", Status: doctor.Pass}, {Name: "bad", Status: doctor.Fail, Evidence: outputSecret}}}
	a := &app{stdout: &stdout, stderr: &stderr, runtimeSecrets: []string{outputSecret}}
	cfg := config.Config{Namespace: "../" + outputSecret, Server: "server"}
	if err := a.writeDoctorReport(cfg, credentials.Set{}, report); err != nil {
		t.Fatal(err)
	}
	var got doctor.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || len(got.Results) != 2 {
		t.Fatalf("fallback lost diagnostics: %s", &stdout)
	}
	if stderr.Len() == 0 || strings.Contains(stdout.String()+stderr.String(), outputSecret) {
		t.Fatalf("unsafe fallback: %s %s", &stdout, &stderr)
	}
	a.stdout = doctorErrorWriter{}
	if err := a.writeDoctorReport(cfg, credentials.Set{}, report); err == nil || strings.Contains(err.Error(), outputSecret) {
		t.Fatalf("stdout error=%v", err)
	}
	for _, full := range []bool{false, true} {
		a.doctorFull = full
		if err := a.writeDoctorReport(config.Config{Namespace: "test", Server: "server"}, credentials.Set{}, report); err == nil || strings.Contains(err.Error(), outputSecret) {
			t.Fatalf("successful-save stdout error=%v", err)
		}
	}
	a.stdout = &stdout
	a.stderr = doctorErrorWriter{}
	if err := a.writeDoctorReport(cfg, credentials.Set{}, report); err == nil || strings.Contains(err.Error(), outputSecret) {
		t.Fatalf("stderr error=%v", err)
	}
}

func TestDoctorOutputFallbackWithoutStderr(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var stdout bytes.Buffer
	a := &app{stdout: &stdout}
	report := doctor.Report{Results: []doctor.Result{{Name: "ok", Status: doctor.Pass}}}
	if err := a.writeDoctorReport(config.Config{Namespace: "../invalid", Server: "server"}, credentials.Set{}, report); err != nil {
		t.Fatal(err)
	}
	var got doctor.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || len(got.Results) != 1 {
		t.Fatalf("fallback lost diagnostics: %s", &stdout)
	}
}

type doctorErrorWriter struct{}

func (doctorErrorWriter) Write([]byte) (int, error) { return 0, errors.New(outputSecret) }
