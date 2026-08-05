package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const expectedStateSchemaVersion = 1

func stateTestPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func TestSaveWritesPrivateJSON(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := Save(path, State{Project: "prod", Server: "web"}); err != nil {
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
	if st.SchemaVersion != 1 || st.Project != "prod" || st.Server != "web" || st.CreatedAt.IsZero() || st.UpdatedAt.IsZero() {
		t.Fatalf("bad state: %+v", st)
	}
}

func TestSaveWritesGenericStateKeys(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := Save(path, State{Project: "prod", Server: "web", Compute: ComputeState{Provider: "hetzner", ID: "2", Name: "prod-web", ProviderState: map[string]string{"access_policy_id": "1"}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"\"namespace\"", "\"compute\"", "\"provider\": \"hetzner\"", "\"access_policy_id\": \"1\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("state missing %q:\n%s", want, text)
		}
	}
	for _, rejected := range []string{"\"project\":", "\"hetzner\":"} {
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
	if err := Save(path, State{Project: "prod", Server: "default"}); err != nil {
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

func TestLoadPreservesUnversionedState(t *testing.T) {
	path := stateTestPath(t, "state.json")
	if err := os.WriteFile(path, []byte(`{"namespace":"prod","server":"web"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.SchemaVersion != expectedStateSchemaVersion || st.Project != "prod" || st.Server != "web" {
		t.Fatalf("bad state: %+v", st)
	}
}

func TestLoadRejectsUnsupportedStateSchema(t *testing.T) {
	for _, version := range []int{-1, expectedStateSchemaVersion + 1} {
		path := stateTestPath(t, "state.json")
		body := []byte(fmt.Sprintf(`{"schema_version":%d,"namespace":"prod","server":"web"}`, version))
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unsupported state schema version") {
			t.Fatalf("Load schema %d error = %v", version, err)
		}
	}
}

func TestSaveRejectsUnsupportedStateSchema(t *testing.T) {
	path := stateTestPath(t, "state.json")
	err := Save(path, State{SchemaVersion: expectedStateSchemaVersion + 1, Project: "prod", Server: "web"})
	if err == nil || !strings.Contains(err.Error(), "unsupported state schema version") {
		t.Fatalf("Save error = %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported state write created file: %v", statErr)
	}
}
