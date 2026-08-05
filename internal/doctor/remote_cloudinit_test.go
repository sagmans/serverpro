package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudInitStatusDoneExitTwoWarns(t *testing.T) {
	r := &scriptedRemote{responses: map[string][]remoteCall{
		"cloud-init status --wait": {{err: errors.New("tailscale ssh failed: exit status 2: status: done")}},
		"cloud-init status --long": {{out: "status: done\nrecoverable errors:\n - package update failed"}},
	}}
	res := remoteCloudInitCheck(context.Background(), r, "deploy", "host", "")
	if res.Status != Warn {
		t.Fatalf("expected warning, got %+v", res)
	}
	if !strings.Contains(res.Evidence, "package update failed") || !strings.Contains(res.Remediation, "cloud-init-output.log") {
		t.Fatalf("missing cloud-init warning detail: %+v", res)
	}
}

func TestCloudInitStatusLongExitTwoStillReportsDeprecationDetail(t *testing.T) {
	detail := "status: done\nextended_status: degraded done\nerrors: []\nrecoverable_errors:\nDEPRECATED:\n\t- Config key 'lists' is deprecated in 22.3 and scheduled to be removed in 27.3. Use 'users' instead.\n\t- The chpasswd multiline string is deprecated in 22.2 and scheduled to be removed in 27.2. Use string type instead."
	r := &scriptedRemote{responses: map[string][]remoteCall{
		"cloud-init status --wait": {{err: errors.New("tailscale ssh failed: exit status 2: status: done")}},
		"cloud-init status --long": {{out: detail, err: errors.New("exit status 2")}},
	}}
	logPath := filepath.Join(t.TempDir(), "cloud-init-status-long.txt")
	res := remoteCloudInitCheck(context.Background(), r, "deploy", "host", logPath)
	if res.Status != Warn {
		t.Fatalf("expected warning for deprecation-only recoverable errors, got %+v", res)
	}
	if !strings.Contains(res.Evidence, "DEPRECATED") || !strings.Contains(res.Remediation, logPath) {
		t.Fatalf("missing cloud-init deprecation detail or saved detail path: %+v", res)
	}
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("missing cloud-init detail log: %v", err)
	}
	if string(got) != detail {
		t.Fatalf("cloud-init detail log truncated or changed:\n%s", got)
	}
}

func TestCloudInitDoesNotTreatOtherStatusCodesAsExitTwo(t *testing.T) {
	r := &scriptedRemote{responses: map[string][]remoteCall{
		cloudInitWaitCommand: {{out: "status: done", err: errors.New("request failed with status 200")}},
	}}
	res := remoteCloudInitCheck(context.Background(), r, "deploy", "host", "")
	if res.Status != Fail {
		t.Fatalf("non-exit status misclassified: %+v", res)
	}
}

func TestCloudInitStatusLongWithHardErrorsStillWarns(t *testing.T) {
	detail := "status: done\nextended_status: degraded done\nerrors:\n - boothook failed\nrecoverable_errors:\nDEPRECATED:\n - old key is deprecated"
	r := &scriptedRemote{responses: map[string][]remoteCall{
		"cloud-init status --wait": {{err: errors.New("tailscale ssh failed: exit status 2: status: done")}},
		"cloud-init status --long": {{out: detail}},
	}}
	res := remoteCloudInitCheck(context.Background(), r, "deploy", "host", "")
	if res.Status != Warn || !strings.Contains(res.Evidence, "boothook failed") {
		t.Fatalf("expected warning for hard errors, got %+v", res)
	}
}
