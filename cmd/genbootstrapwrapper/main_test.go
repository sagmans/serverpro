package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
)

func TestMainPrintsWrapperScript(t *testing.T) {
	// WHY: `make gen-bootstrap-wrapper` covers the built command, but package
	// coverage only sees main when invoked from a Go test.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.Stdout = oldStdout
		_ = r.Close()
	}()

	os.Stdout = w
	main()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != bootstraptools.WrapperScript() || !strings.HasPrefix(string(body), "#!") {
		t.Fatalf("wrapper output drifted from the pin manifest: %d bytes", len(body))
	}
}
