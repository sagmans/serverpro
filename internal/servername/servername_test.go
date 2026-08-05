package servername

import "testing"

func TestNormalizeUsesDefaultForEmptyServer(t *testing.T) {
	if Normalize("") != Default {
		t.Fatalf("empty server did not normalize to %q", Default)
	}
	if Normalize("web") != "web" {
		t.Fatal("named server should be preserved")
	}
}
