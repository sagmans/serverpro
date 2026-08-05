package remote

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchScriptRunsEveryCommandAndFramesResults(t *testing.T) {
	commands := []BatchCommand{{Script: "printf first"}, {Script: "printf second >&2; exit 7"}}
	out, err := exec.Command("sh", "-c", batchScript(commands)).CombinedOutput()
	if err != nil {
		t.Fatalf("batch script failed: %v\n%s", err, out)
	}
	results, err := parseBatchOutput(string(out), len(commands))
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Output != "first" || results[0].Err != nil {
		t.Fatalf("first result = %+v", results[0])
	}
	if results[1].Output != "second" || results[1].Err == nil || !strings.Contains(results[1].Err.Error(), "status 7") {
		t.Fatalf("second result = %+v", results[1])
	}
}

func TestBatchScriptBoundsEachCommandOutput(t *testing.T) {
	command := fmt.Sprintf("yes x | head -c %d", maxBatchCommandOutputBytes+1)
	out, err := exec.Command("sh", "-c", batchScript([]BatchCommand{{Script: command}})).CombinedOutput()
	if err != nil {
		t.Fatalf("batch script failed: %v\n%s", err, out)
	}
	results, err := parseBatchOutput(string(out), 1)
	if err != nil {
		t.Fatal(err)
	}
	var limitErr *BatchCommandOutputLimitError
	if len(results) != 1 || !errors.As(results[0].Err, &limitErr) {
		t.Fatalf("batch result = %+v frame=%q, want typed command-output limit error", results, out)
	}
	if results[0].Output != "" || limitErr.Index != 0 || limitErr.Limit != maxBatchCommandOutputBytes {
		t.Fatalf("limit result=%+v error=%+v", results[0], limitErr)
	}
}

func TestParseBatchOutputStopsAtCommandAndAggregateLimits(t *testing.T) {
	oversized := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", maxBatchCommandOutputBytes+1)))
	_, err := parseBatchOutput(fmt.Sprintf("%s\t0\t0\t0\t%s\n", batchFramePrefix, oversized), 1)
	var commandErr *BatchCommandOutputLimitError
	if !errors.As(err, &commandErr) {
		t.Fatalf("command overflow error = %T %v", err, err)
	}

	const aggregateFrameCount = 5
	chunk := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", maxBatchOutputBytes/aggregateFrameCount+1)))
	var frames strings.Builder
	for index := range aggregateFrameCount {
		fmt.Fprintf(&frames, "%s\t%d\t0\t0\t%s\n", batchFramePrefix, index, chunk)
	}
	_, err = parseBatchOutput(frames.String(), aggregateFrameCount)
	var aggregateErr *BatchOutputLimitError
	if !errors.As(err, &aggregateErr) || aggregateErr.Limit != maxBatchOutputBytes {
		t.Fatalf("aggregate overflow error = %T %+v", err, aggregateErr)
	}
}

func TestParseBatchOutputRejectsMalformedFrames(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "count", text: ""},
		{name: "prefix", text: "other\t0\t0\t0\tb2s=\n"},
		{name: "index", text: batchFramePrefix + "\t1\t0\t0\tb2s=\n"},
		{name: "status", text: batchFramePrefix + "\t0\tinvalid\t0\tb2s=\n"},
		{name: "overflow", text: batchFramePrefix + "\t0\t0\tinvalid\tb2s=\n"},
		{name: "output", text: batchFramePrefix + "\t0\t0\t0\t%%%\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseBatchOutput(tc.text, 1); err == nil {
				t.Fatalf("parseBatchOutput(%q) succeeded", tc.text)
			}
		})
	}
}

func TestTailscaleSSHRunBatchUsesOneProcess(t *testing.T) {
	dir := t.TempDir()
	callsPath := filepath.Join(dir, "calls")
	program := "#!/bin/sh\nset -eu\nprintf call >> " + testShellQuote(callsPath) + "\nprintf '" + batchFramePrefix + "\\t0\\t0\\t0\\tb2s=\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	results, err := (TailscaleSSH{SudoPassword: "secret"}).RunBatch(context.Background(), "deploy", "prod-01", []BatchCommand{{Script: "printf ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Output != "ok" || results[0].Err != nil {
		t.Fatalf("batch results = %+v", results)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "call" {
		t.Fatalf("tailscale calls = %q", calls)
	}
}

func TestTailscaleSSHRunBatchRequiresPassword(t *testing.T) {
	_, err := (TailscaleSSH{}).RunBatch(context.Background(), "deploy", "prod-01", []BatchCommand{{Script: "true"}})
	if err == nil || !strings.Contains(err.Error(), "sudo password required") {
		t.Fatalf("expected sudo password error, got %v", err)
	}
}

func TestTailscaleSSHRunBatchReturnsTransportFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte("#!/bin/sh\nexit 9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	_, err := (TailscaleSSH{SudoPassword: "secret"}).RunBatch(context.Background(), "deploy", "prod-01", []BatchCommand{{Script: "true"}})
	if err == nil || !strings.Contains(err.Error(), "tailscale ssh failed") {
		t.Fatalf("expected transport failure, got %v", err)
	}
}

func TestTailscaleSSHRunBatchBoundsTransportOutput(t *testing.T) {
	dir := t.TempDir()
	program := fmt.Sprintf("#!/bin/sh\nyes x | head -c %d\n", maxBatchWireOutputBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	_, err := (TailscaleSSH{SudoPassword: "secret"}).RunBatch(context.Background(), "deploy", "prod-01", []BatchCommand{{Script: "true"}})
	var limitErr *BatchOutputLimitError
	if !errors.As(err, &limitErr) || limitErr.Limit != maxBatchWireOutputBytes {
		t.Fatalf("transport overflow error = %T %v; typed=%+v", err, err, limitErr)
	}
}

func TestTailscaleSSHRunBatchSkipsProcessForNoCommands(t *testing.T) {
	results, err := (TailscaleSSH{}).RunBatch(context.Background(), "deploy", "prod-01", nil)
	if err != nil || len(results) != 0 {
		t.Fatalf("empty batch results=%+v err=%v", results, err)
	}
}

func TestBatchCommandErrorsExcludeScriptAndOutput(t *testing.T) {
	secret := "cloudflare-token"
	results, err := parseBatchOutput(batchFramePrefix+"\t0\t1\t0\t"+"Y2xvdWRmbGFyZS10b2tlbg=="+"\n", 1)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Err == nil || errors.Is(results[0].Err, context.Canceled) {
		t.Fatalf("missing command failure: %+v", results[0])
	}
	if strings.Contains(results[0].Err.Error(), secret) {
		t.Fatalf("batch error leaked output: %v", results[0].Err)
	}
}

func TestBatchCommandOutputLimitErrorMessage(t *testing.T) {
	err := (&BatchCommandOutputLimitError{Index: 2, Limit: 8}).Error()
	if err != "remote batch command 2 output exceeds 8-byte limit" {
		t.Fatalf("command limit error = %q", err)
	}
}

func TestBatchOutputLimitErrorMessage(t *testing.T) {
	err := (&BatchOutputLimitError{Limit: 8}).Error()
	if err != "remote batch output exceeds 8-byte limit" {
		t.Fatalf("batch limit error = %q", err)
	}
}
