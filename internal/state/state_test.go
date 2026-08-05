package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stateTestPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func TestSaveWritesPrivateJSON(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := Save(path, State{Namespace: "prod", Server: "web"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.SchemaVersion != 1 || st.Namespace != "prod" || st.Server != "web" || st.CreatedAt.IsZero() || st.UpdatedAt.IsZero() {
		t.Fatalf("bad state: %+v", st)
	}
}

func TestUpdateReadModifyWritesState(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := Save(path, State{Namespace: "prod", Server: "web", Compute: ComputeState{PublicIPv4: "203.0.113.1"}}); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, func(st *State) error {
		st.Ingress = append(st.Ingress, IngressState{Type: "cloudflare-tunnel", Hostname: "app.example.com"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Compute.PublicIPv4 != "203.0.113.1" || len(st.Ingress) != 1 {
		t.Fatalf("state = %+v", st)
	}
}

func TestSaveWritesGenericStateKeys(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := Save(path, State{Namespace: "prod", Server: "web", Compute: ComputeState{Provider: "hetzner", ID: "2", Name: "prod-web", ProviderState: map[string]string{"access_policy_id": "1"}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"\"namespace\"", "\"compute\"", "\"provider\": \"hetzner\"", "\"kind\": \"access_policy\"", "\"id\": \"1\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("state missing %q:\n%s", want, text)
		}
	}
	for _, rejected := range []string{"\"project\":", "\"hetzner\":", "\"access_policy_id\":"} {
		if strings.Contains(text, rejected) {
			t.Fatalf("state contains retired key %q:\n%s", rejected, text)
		}
	}
}

func TestSaveResetsExistingFilePermissions(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := os.WriteFile(path, []byte(`{"namespace":"prod"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, State{Namespace: "prod", Server: "default"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestRemoveDurablySyncsStateDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	path := stateTestPath(t, "state.json")
	if err := os.WriteFile(path, []byte(`{"namespace":"prod","server":"web"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if err := RemoveDurably(path); err == nil {
		t.Fatal("state removal did not report parent-directory sync failure")
	}
}

func TestLoadMigratesLegacyProjectField(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := os.WriteFile(path, []byte(`{"project":"prod","server":"web"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Namespace != "prod" || st.Server != "web" {
		t.Fatalf("state = %+v", st)
	}
	if err := Save(path, st); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"project"`) || !strings.Contains(string(body), `"namespace": "prod"`) {
		t.Fatalf("state migration = %s", body)
	}
}

func TestLoadRejectsDivergentNamespaceAndLegacyProject(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := os.WriteFile(path, []byte(`{"namespace":"prod","project":"other","server":"web"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `state namespace "prod" conflicts with legacy project "other"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadPreservesStateServer(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := os.WriteFile(path, []byte(`{"namespace":"prod","server":"web"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Namespace != "prod" || st.Server != "web" {
		t.Fatalf("bad state: %+v", st)
	}
}
