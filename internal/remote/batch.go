package remote

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/shell"
)

const (
	batchFramePrefix              = "serverpro-batch-v2"
	maxExitStatus                 = 255
	maxBatchCommandOutputBytes    = 1 << 20
	maxBatchOutputBytes           = 4 << 20
	maxBatchWireOutputBytes       = 6 << 20
	batchOutputWithinLimit        = 0
	batchCommandOutputExceeded    = 1
	batchCommandOutputReadPadding = 1
)

// BatchCommandOutputLimitError reports one command exceeding its decoded output ceiling.
type BatchCommandOutputLimitError struct {
	Index int
	Limit int
}

func (e *BatchCommandOutputLimitError) Error() string {
	return fmt.Sprintf("remote batch command %d output exceeds %d-byte limit", e.Index, e.Limit)
}

// BatchOutputLimitError reports aggregate decoded or framed transport output overflow.
type BatchOutputLimitError struct {
	Limit int
}

func (e *BatchOutputLimitError) Error() string {
	return fmt.Sprintf("remote batch output exceeds %d-byte limit", e.Limit)
}

type BatchCommand struct {
	Script string
}

type BatchResult struct {
	Output string
	Err    error
}

type BatchRunner interface {
	Runner
	RunBatch(context.Context, string, string, []BatchCommand) ([]BatchResult, error)
}

// RunBatch amortizes transport and sudo authentication while preserving one
// independent exit status per read-only command.
func (r TailscaleSSH) RunBatch(ctx context.Context, user, host string, commands []BatchCommand) ([]BatchResult, error) {
	if len(commands) == 0 {
		return []BatchResult{}, nil
	}
	if r.SudoPassword == "" {
		return nil, fmt.Errorf("sudo password required")
	}
	out, err := r.runWithInputLimit(ctx, user, host, batchScript(commands), "", maxBatchWireOutputBytes)
	if err != nil {
		return nil, err
	}
	return parseBatchOutput(out, len(commands))
}

func batchScript(commands []BatchCommand) string {
	var script strings.Builder
	script.WriteString(`set +e
batch_tmp="$(mktemp -d)"
cleanup_batch() { rm -rf "$batch_tmp"; }
trap cleanup_batch EXIT
run_batch_command() {
  index="$1"
  encoded="$2"
  status_path="$batch_tmp/status-$index"
  output="$(
    (
      printf '%s' "$encoded" | base64 -d | sh 2>&1
      printf '%s' "$?" > "$status_path"
    ) | head -c `)
	fmt.Fprintf(&script, "%d", maxBatchCommandOutputBytes+batchCommandOutputReadPadding)
	script.WriteString(`
  )"
  status="$(cat "$status_path")"
  output_size="$(LC_ALL=C printf '%s' "$output" | wc -c)"
  overflow=`)
	fmt.Fprintf(&script, "%d", batchOutputWithinLimit)
	script.WriteString(`
  if [ "$output_size" -gt `)
	fmt.Fprintf(&script, "%d", maxBatchCommandOutputBytes)
	script.WriteString(` ]; then
    overflow=`)
	fmt.Fprintf(&script, "%d", batchCommandOutputExceeded)
	script.WriteString(`
    output=''
  fi
  encoded_output="$(printf '%s' "$output" | base64 | tr -d '\n')"
  printf '`)
	script.WriteString(batchFramePrefix)
	script.WriteString(`\t%s\t%s\t%s\t%s\n' "$index" "$status" "$overflow" "$encoded_output"
}
`)
	for index, command := range commands {
		encoded := base64.StdEncoding.EncodeToString([]byte(command.Script))
		fmt.Fprintf(&script, "run_batch_command %d %s\n", index, shell.Quote(encoded))
	}
	return script.String()
}

func parseBatchOutput(text string, count int) ([]BatchResult, error) {
	if len(text) > maxBatchWireOutputBytes {
		return nil, &BatchOutputLimitError{Limit: maxBatchWireOutputBytes}
	}
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		if count == 0 {
			return []BatchResult{}, nil
		}
		return nil, fmt.Errorf("remote batch returned 0 frames; want %d", count)
	}
	lines := strings.Split(text, "\n")
	if len(lines) != count {
		return nil, fmt.Errorf("remote batch returned %d frames; want %d", len(lines), count)
	}
	results := make([]BatchResult, count)
	totalOutputBytes := 0
	for expectedIndex, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || fields[0] != batchFramePrefix {
			return nil, fmt.Errorf("remote batch frame %d is malformed", expectedIndex)
		}
		index, err := strconv.Atoi(fields[1])
		if err != nil || index != expectedIndex {
			return nil, fmt.Errorf("remote batch frame %d has invalid index", expectedIndex)
		}
		status, err := strconv.Atoi(fields[2])
		if err != nil || status < 0 || status > maxExitStatus {
			return nil, fmt.Errorf("remote batch frame %d has invalid status", expectedIndex)
		}
		overflow, err := strconv.Atoi(fields[3])
		if err != nil || (overflow != batchOutputWithinLimit && overflow != batchCommandOutputExceeded) {
			return nil, fmt.Errorf("remote batch frame %d has invalid overflow status", expectedIndex)
		}
		if overflow == batchCommandOutputExceeded {
			if fields[4] != "" {
				return nil, fmt.Errorf("remote batch frame %d is malformed", expectedIndex)
			}
			results[index].Err = &BatchCommandOutputLimitError{Index: index, Limit: maxBatchCommandOutputBytes}
			continue
		}
		output, err := decodeBatchCommandOutput(fields[4], index)
		if err != nil {
			return nil, err
		}
		if len(output) > maxBatchOutputBytes-totalOutputBytes {
			return nil, &BatchOutputLimitError{Limit: maxBatchOutputBytes}
		}
		totalOutputBytes += len(output)
		results[index].Output = string(output)
		if status != 0 {
			// WHY: scripts and command output may contain credentials; callers get
			// enough stable evidence to classify the failed check without leakage.
			results[index].Err = fmt.Errorf("remote batch command %d failed with status %d", index, status)
		}
	}
	return results, nil
}

func decodeBatchCommandOutput(encoded string, index int) ([]byte, error) {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	output, err := io.ReadAll(io.LimitReader(decoder, maxBatchCommandOutputBytes+batchCommandOutputReadPadding))
	if err != nil {
		return nil, fmt.Errorf("remote batch frame %d has invalid output", index)
	}
	if len(output) > maxBatchCommandOutputBytes {
		return nil, &BatchCommandOutputLimitError{Index: index, Limit: maxBatchCommandOutputBytes}
	}
	return output, nil
}
