package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

// WHY: `namespace status` resolves a namespace and reports its server count. Pin
// the happy path, the invalid-name guard, and the not-found guard so operators
// get deterministic JSON and clear errors.

func TestNamespaceStatusReportsServerCount(t *testing.T) {
	createTestHome(t)
	reg := state.NewRegistry()
	reg.UpsertNamespace("demoapp")
	reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "web", StatePath: config.ServerStatePath("demoapp", "web")})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"namespace", "status", "demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row struct {
		Namespace   string `json:"namespace"`
		ServerCount int    `json:"server_count"`
	}
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("namespace status output is not JSON: %s", out.String())
	}
	if row.Namespace != "demoapp" || row.ServerCount != 1 {
		t.Fatalf("bad status row: %+v", row)
	}
}

func TestNamespaceStatusRejectsInvalidName(t *testing.T) {
	createTestHome(t)
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"namespace", "status", "Bad Name"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("expected invalid namespace error, got %v", err)
	}
}

func TestNamespaceStatusReportsMissingNamespace(t *testing.T) {
	createTestHome(t)
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"namespace", "status", "ghost"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}
