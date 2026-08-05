// Package shell provides small helpers for composing POSIX shell snippets.
package shell

import "strings"

// Quote wraps s in POSIX single quotes so it is safe to splice into a shell
// script. Any embedded single quotes are escaped using the standard idiom of
// closing the quoted string, emitting an escaped quote, and reopening.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
