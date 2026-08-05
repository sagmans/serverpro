package state

import (
	"os"
	"path/filepath"
	"testing"
)

// WHY: import/recovery gates writes on Exists; a false positive would silently
// clobber managed state, so pin both branches in the owning package.

func TestExistsReportsPresentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, err := Exists(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("Exists should report true for an existing file")
	}
}

func TestExistsReportsMissingFile(t *testing.T) {
	exists, err := Exists(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("Exists should report false for a missing file")
	}
}

func TestExistsReturnsNonNotFoundErrors(t *testing.T) {
	// WHY: a path containing NUL fails deterministically on every supported OS,
	// unlike permission fixtures that can pass when tests run as root.
	exists, err := Exists("invalid\x00state")
	if err == nil {
		t.Fatal("Exists should surface non-not-found stat errors")
	}
	if exists {
		t.Fatal("invalid path should not report an existing state")
	}
}
