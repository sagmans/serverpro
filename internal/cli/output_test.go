package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteJSONWritesIndentedPayload(t *testing.T) {
	var out bytes.Buffer
	row := struct {
		Name string `json:"name"`
	}{Name: "web"}
	if err := writeJSON(&out, row); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %q", out.String())
	}
	if decoded.Name != "web" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestWriteJSONEncodesNilSlicesAsEmptyArrays(t *testing.T) {
	var out bytes.Buffer
	var rows []struct {
		Name string `json:"name"`
	}
	if err := writeJSON(&out, rows); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Fatalf("nil slice encoded as %q, want []", out.String())
	}
}

func TestWriteJSONEncodesNilSliceFieldsAsEmptyArrays(t *testing.T) {
	var out bytes.Buffer
	row := struct {
		Items []string `json:"items"`
	}{}
	if err := writeJSON(&out, row); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\n  \"items\": []\n}\n" {
		t.Fatalf("nil slice field encoded as %q", out.String())
	}
}

func TestWriteJSONNormalizesNilSlicesInsideInterfaceFields(t *testing.T) {
	// WHY: exercises normalizeJSONInterface — an any-typed field holding a struct
	// whose nil slice must still render as [] rather than null.
	type inner struct {
		Items []string `json:"items"`
	}
	var out bytes.Buffer
	row := struct {
		Payload any `json:"payload"`
	}{Payload: inner{}}
	if err := writeJSON(&out, row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "null") || !strings.Contains(out.String(), "\"items\": []") {
		t.Fatalf("interface field nil slice not normalized:\n%s", out.String())
	}
}

func TestWriteJSONNormalizesNilSlicesInsideArrays(t *testing.T) {
	// WHY: exercises normalizeJSONArray — a fixed-size Go array of structs whose
	// nil slice fields must render as [].
	type inner struct {
		Items []string `json:"items"`
	}
	var out bytes.Buffer
	row := struct {
		Pairs [2]inner `json:"pairs"`
	}{}
	if err := writeJSON(&out, row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "null") || strings.Count(out.String(), "\"items\": []") != 2 {
		t.Fatalf("array element nil slices not normalized:\n%s", out.String())
	}
}

func TestWriteJSONNormalizesNilSlicesBehindPointerFields(t *testing.T) {
	// WHY: writeJSON accepts arbitrary payload shapes; pointer fields must keep
	// the same nil-slice normalization contract as direct struct fields.
	type inner struct {
		Items []string `json:"items"`
	}
	var out bytes.Buffer
	row := struct {
		Enabled *inner `json:"enabled"`
	}{Enabled: &inner{}}
	if err := writeJSON(&out, row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "null") || !strings.Contains(out.String(), "\"items\": []") {
		t.Fatalf("pointer field nil slice not normalized:\n%s", out.String())
	}
}

func TestWriteJSONEncodesNestedNilSlicesAsEmptyArrays(t *testing.T) {
	var out bytes.Buffer
	row := struct {
		Groups map[string]struct {
			Items []string `json:"items"`
		} `json:"groups"`
	}{Groups: map[string]struct {
		Items []string `json:"items"`
	}{"default": {}}}
	if err := writeJSON(&out, row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "null") || !strings.Contains(out.String(), "\"items\": []") {
		t.Fatalf("nested nil slice not normalized:\n%s", out.String())
	}
}
