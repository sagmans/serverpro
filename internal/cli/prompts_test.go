package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPromptDefaultNonInteractiveRequiresValue(t *testing.T) {
	a := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	if _, err := a.promptDefault("namespace", ""); err == nil || !strings.Contains(err.Error(), "namespace required") {
		t.Fatalf("expected required error, got %v", err)
	}
	value, err := a.promptDefault("namespace", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if value != "demo" {
		t.Fatalf("value = %q", value)
	}
}

func TestPromptChoiceNonInteractiveUsesDefault(t *testing.T) {
	a := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	value, err := a.promptChoice("server type", "cax11", []choice{{Value: "cx23", Description: "x86"}})
	if err != nil {
		t.Fatal(err)
	}
	if value != "cax11" {
		t.Fatalf("value = %q", value)
	}
}

func TestPromptNonInteractiveDoesNotReadStdin(t *testing.T) {
	a := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	if _, err := a.prompt("provider token"); err == nil || !strings.Contains(err.Error(), "provider token required") {
		t.Fatalf("expected required error, got %v", err)
	}
}

func TestConfirmNonInteractiveDoesNotPrompt(t *testing.T) {
	var out bytes.Buffer
	a := &app{nonInteractive: true, stdin: strings.NewReader("yes\n"), stdout: &out}
	if err := a.confirm("Destroy state-known serverpro resources?"); err == nil || !strings.Contains(err.Error(), "--yes required") {
		t.Fatalf("expected --yes error, got %v", err)
	}
	if out.String() != "" {
		t.Fatalf("prompt written in non-interactive mode: %q", out.String())
	}
}

func TestChoiceValueFromAnswerMapsFallbackNumber(t *testing.T) {
	choices := []choice{{Value: "fsn1"}, {Value: "nbg1"}}
	if got := choiceValueFromAnswer("2", choices); got != "nbg1" {
		t.Fatalf("numeric choice = %q", got)
	}
	if got := choiceValueFromAnswer("ash", choices); got != "ash" {
		t.Fatalf("literal choice = %q", got)
	}
}

func TestFZFChoiceReturnsSelectedValue(t *testing.T) {
	// WHY: interactive catalog/server selection may use fzf when available. Fake
	// the executable so the parser contract is covered without needing a TTY.
	dir := t.TempDir()
	path := filepath.Join(dir, "fzf")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'nbg1\\tNuremberg\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, ok := (&app{}).fzfChoice("location", []choice{{Value: "fsn1", Description: "Falkenstein"}, {Value: "nbg1", Description: "Nuremberg"}})
	if !ok || got != "nbg1" {
		t.Fatalf("fzf choice = %q ok=%t", got, ok)
	}
}

func TestFZFChoiceReturnsFalseWhenUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got, ok := (&app{}).fzfChoice("location", []choice{{Value: "fsn1"}})
	if ok || got != "" {
		t.Fatalf("missing fzf should return no selection, got %q ok=%t", got, ok)
	}
}

func TestConfirmWritesPromptToStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	a := &app{stdin: strings.NewReader("yes\n"), stdout: &out, stderr: &errOut}
	if err := a.confirm("Destroy state-known serverpro resources?"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Fatalf("json stdout polluted by prompt: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Destroy state-known serverpro resources?") {
		t.Fatalf("prompt missing from stderr: %q", errOut.String())
	}
}

func TestSplitCSVTrimsAndDropsEmptyParts(t *testing.T) {
	got := splitCSV(" tag:demo , ,tag:ssh,, tag:web ")
	want := []string{"tag:demo", "tag:ssh", "tag:web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV = %#v, want %#v", got, want)
	}
}
