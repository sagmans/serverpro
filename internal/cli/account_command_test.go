package cli

import (
	"io"
	"strings"
	"testing"
)

func TestAccountCommandIsRemoved(t *testing.T) {
	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"account", "list"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected removed account command rejection, got %v", err)
	}
}
