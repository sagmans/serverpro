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

	// The wrapper carries the Pi lock and outgrows the pipe buffer, so the
	// reader must drain concurrently or main blocks on its own write.
	type result struct {
		body []byte
		err  error
	}
	read := make(chan result, 1)
	go func() {
		body, err := io.ReadAll(r)
		read <- result{body, err}
	}()
	os.Stdout = w
	main()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := <-read
	if got.err != nil {
		t.Fatal(got.err)
	}
	body := got.body
	if string(body) != bootstraptools.WrapperScript() || !strings.HasPrefix(string(body), "#!") {
		t.Fatalf("wrapper output drifted from the pin manifest: %d bytes", len(body))
	}
}
