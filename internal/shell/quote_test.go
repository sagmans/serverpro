package shell

import "testing"

func TestQuoteEscapesEmbeddedSingleQuotes(t *testing.T) {
	cases := map[string]string{
		"":              "''",
		"plain":         "'plain'",
		"prod's web":    `'prod'\''s web'`,
		"a'b'c":         `'a'\''b'\''c'`,
		"with $var":     "'with $var'",
		`back\slash`:    `'back\slash'`,
		"double\"quote": `'double"quote'`,
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Fatalf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}
