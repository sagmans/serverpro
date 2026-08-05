package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// WHY: `serverpro doctor` is the global preflight. Prove it emits the three
// provider/default-ingress/tailscale-ssh rows as stable JSON and that --fix is
// rejected at the global scope (fix is server-scoped only).

func TestGlobalDoctorEmitsProviderAndSecurityRows(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Status  string `json:"status"`
		Scope   string `json:"scope"`
		Count   int    `json:"count"`
		Value   string `json:"value"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("doctor output is not JSON: %s", out.String())
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %+v", rows)
	}
	scopes := map[string]bool{}
	for _, row := range rows {
		scopes[row.Scope] = true
		if row.Status != "pass" {
			t.Fatalf("scope %q not pass: %+v", row.Scope, row)
		}
	}
	for _, want := range []string{"providers", "default_ingress", "tailscale_ssh"} {
		if !scopes[want] {
			t.Fatalf("missing scope %q in %+v", want, rows)
		}
	}
}

func TestGlobalDoctorRejectsFixFlag(t *testing.T) {
	createTestHome(t)
	var errOut bytes.Buffer
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"doctor", "--fix"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--fix is only supported") {
		t.Fatalf("expected --fix rejection, got %v", err)
	}
}
