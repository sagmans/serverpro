package privatefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteJSONWritesPrivateFileAndDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data.json")
	if err := AtomicWriteJSON(path, map[string]string{"name": "prod"}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != DefaultFileMode {
		t.Fatalf("file mode = %o", fileInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != DefaultDirMode {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}
}

func TestReadJSONRejectsWorldReadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(`{"name":"prod"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	err := ReadJSON(path, &out, "test")
	if err == nil || !strings.Contains(err.Error(), "group/world accessible") {
		t.Fatalf("expected private-file error, got %v", err)
	}
}

func TestRejectSymlinkPathRejectsAncestor(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	err := RejectSymlinkPath(filepath.Join(link, "data.json"), home, "test", "save")
	if err == nil || !strings.Contains(err.Error(), "symlink test path") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
}
