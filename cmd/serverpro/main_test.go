package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestMainPrintsVersion(t *testing.T) {
	// WHY: smoke tests cover the built binary, but package coverage only sees
	// main when invoked from a Go test. Use the harmless version path.
	oldArgs := os.Args
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
		_ = r.Close()
	}()

	os.Args = []string{"serverpro", "--version"}
	os.Stdout = w
	main()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "serverpro version") {
		t.Fatalf("version output = %q", body)
	}
}
