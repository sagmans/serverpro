package tailscale

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

// RFC 6902 operations that policyPatch emits. hujson.Patch applies them in
// place, so only the entries serverpro edits lose their surrounding text.
const (
	patchOpAdd     = "add"
	patchOpRemove  = "remove"
	patchOpReplace = "replace"
)

// maxArrayDiffCells caps the LCS table of one array diff. A policy array
// above it is replaced whole: its comments go, but the write stays correct
// and the diff cannot use unbounded memory on a hostile or huge policy.
const maxArrayDiffCells = 1 << 20

// ErrPolicyRenderMismatch means the patched HuJSON file does not decode to
// the document that serverpro computed. The write stops, because posting it
// would publish a policy that nobody reviewed.
var ErrPolicyRenderMismatch = errors.New("tailnet policy patch does not reproduce the computed policy; refusing update")

// jsonPointerEscaper encodes object keys as RFC 6901 reference tokens.
// Tailscale keys such as "tag:x" are safe, but a key with "/" or "~" must not
// point into a different member.
var jsonPointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

type patchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}

// renderPolicyFile turns the edited document back into the operator's HuJSON
// file. It patches only the values that changed, so comments and key order
// elsewhere stay, then formats the file the same way the admin console does.
func renderPolicyFile(file policyFile) ([]byte, error) {
	before := standardJSON(file.source)
	after, err := json.Marshal(file.doc)
	if err != nil {
		return nil, fmt.Errorf("encode tailnet policy: %w", err)
	}
	patch, err := policyPatch(before, after)
	if err != nil {
		return nil, err
	}
	out := file.source.Clone()
	if err := out.Patch(patch); err != nil {
		return nil, fmt.Errorf("patch tailnet policy HuJSON: %w", err)
	}
	out.Format()
	if err := requireSameJSON(out, after); err != nil {
		return nil, err
	}
	return out.Pack(), nil
}

// standardJSON is the comment-free view of a HuJSON file that the document
// helpers and the diff both read.
func standardJSON(v hujson.Value) []byte {
	std := v.Clone()
	std.Standardize()
	return std.Pack()
}

// requireSameJSON is the fail-closed check behind ErrPolicyRenderMismatch.
func requireSameJSON(out hujson.Value, want []byte) error {
	gotValue, err := decodeJSONValue(standardJSON(out))
	if err != nil {
		return fmt.Errorf("decode patched tailnet policy: %w", err)
	}
	wantValue, err := decodeJSONValue(want)
	if err != nil {
		return fmt.Errorf("decode computed tailnet policy: %w", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		return ErrPolicyRenderMismatch
	}
	return nil
}

// decodeJSONValue keeps numbers as json.Number, so a diff never sees a float
// rounding change that the operator did not make.
func decodeJSONValue(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// policyPatch is the smallest practical RFC 6902 patch from before to after.
func policyPatch(before, after []byte) ([]byte, error) {
	beforeValue, err := decodeJSONValue(before)
	if err != nil {
		return nil, fmt.Errorf("decode current tailnet policy: %w", err)
	}
	afterValue, err := decodeJSONValue(after)
	if err != nil {
		return nil, fmt.Errorf("decode computed tailnet policy: %w", err)
	}
	d := patchDiffer{}
	if err := d.value("", beforeValue, afterValue); err != nil {
		return nil, err
	}
	if d.ops == nil {
		d.ops = []patchOperation{}
	}
	return marshalPatchValue(d.ops)
}

type patchDiffer struct {
	ops []patchOperation
}

func (d *patchDiffer) value(path string, before, after any) error {
	if reflect.DeepEqual(before, after) {
		return nil
	}
	switch b := before.(type) {
	case map[string]any:
		if a, ok := after.(map[string]any); ok {
			return d.object(path, b, a)
		}
	case []any:
		if a, ok := after.([]any); ok {
			return d.array(path, b, a)
		}
	}
	if path == "" {
		return fmt.Errorf("tailnet policy root must stay a JSON object")
	}
	return d.emit(patchOpReplace, path, after)
}

// object walks keys in sorted order so the same edit always yields the same
// patch. New keys go to the end of the object, as the admin console does.
func (d *patchDiffer) object(path string, before, after map[string]any) error {
	for _, key := range sortedKeys(before) {
		child := path + "/" + jsonPointerEscaper.Replace(key)
		next, ok := after[key]
		if !ok {
			if err := d.emit(patchOpRemove, child, nil); err != nil {
				return err
			}
			continue
		}
		if err := d.value(child, before[key], next); err != nil {
			return err
		}
	}
	for _, key := range sortedKeys(after) {
		if _, ok := before[key]; ok {
			continue
		}
		if err := d.emit(patchOpAdd, path+"/"+jsonPointerEscaper.Replace(key), after[key]); err != nil {
			return err
		}
	}
	return nil
}

// array aligns elements by their longest common subsequence. Unchanged
// elements keep their comments; a changed element of the same shape is
// diffed in place, so a trimmed dst list keeps the comments of its rule.
func (d *patchDiffer) array(path string, before, after []any) error {
	if len(before)*len(after) > maxArrayDiffCells {
		return d.emit(patchOpReplace, path, after)
	}
	lcs := lcsTable(before, after)
	i, j, index := 0, 0, 0
	for i < len(before) || j < len(after) {
		if i < len(before) && j < len(after) && reflect.DeepEqual(before[i], after[j]) {
			i, j, index = i+1, j+1, index+1
			continue
		}
		var removed, added []any
		for i < len(before) || j < len(after) {
			if i < len(before) && j < len(after) && reflect.DeepEqual(before[i], after[j]) {
				break
			}
			if j < len(after) && (i == len(before) || lcs[i][j+1] >= lcs[i+1][j]) {
				added = append(added, after[j])
				j++
				continue
			}
			removed = append(removed, before[i])
			i++
		}
		next, err := d.arrayRun(path, index, removed, added)
		if err != nil {
			return err
		}
		index = next
	}
	return nil
}

// arrayRun turns one run of removed and added elements into operations at
// index and returns the index after the run.
func (d *patchDiffer) arrayRun(path string, index int, removed, added []any) (int, error) {
	paired := min(len(removed), len(added))
	for k := range paired {
		if err := d.value(path+"/"+strconv.Itoa(index), removed[k], added[k]); err != nil {
			return 0, err
		}
		index++
	}
	for range removed[paired:] {
		if err := d.emit(patchOpRemove, path+"/"+strconv.Itoa(index), nil); err != nil {
			return 0, err
		}
	}
	for _, value := range added[paired:] {
		if err := d.emit(patchOpAdd, path+"/"+strconv.Itoa(index), value); err != nil {
			return 0, err
		}
		index++
	}
	return index, nil
}

func (d *patchDiffer) emit(op, path string, value any) error {
	operation := patchOperation{Op: op, Path: path}
	if op != patchOpRemove {
		raw, err := marshalPatchValue(value)
		if err != nil {
			return err
		}
		operation.Value = raw
	}
	d.ops = append(d.ops, operation)
	return nil
}

// marshalPatchValue keeps "<", ">" and "&" literal: patch values land in the
// operator's file, where unicode escapes of them would be noise. The patch
// itself goes through it too, because json.Marshal would escape its raw
// values again.
func marshalPatchValue(value any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, fmt.Errorf("encode tailnet policy patch value: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// lcsTable holds, at [i][j], the common subsequence length of before[i:]
// and after[j:]; array reads it to pick removals or additions.
func lcsTable(before, after []any) [][]int {
	table := make([][]int, len(before)+1)
	for i := range table {
		table[i] = make([]int, len(after)+1)
	}
	for i := len(before) - 1; i >= 0; i-- {
		for j := len(after) - 1; j >= 0; j-- {
			if reflect.DeepEqual(before[i], after[j]) {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			table[i][j] = max(table[i+1][j], table[i][j+1])
		}
	}
	return table
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
