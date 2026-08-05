package lifecycle

import (
	"path/filepath"
	"testing"
)

func provisionStatePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state.json")
}
