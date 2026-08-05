package cli

import (
	"fmt"
	"os"

	"github.com/assagman/serverpro/internal/filedescriptor"
	"golang.org/x/term"
)

func (a *app) promptSecret(label string) (string, error) {
	if a.nonInteractive {
		return "", fmt.Errorf("%s required in non-interactive mode", label)
	}
	if f, ok := a.stdin.(*os.File); ok {
		fd, err := filedescriptor.Int(f)
		if err == nil && term.IsTerminal(fd) {
			if _, err := fmt.Fprintf(a.promptWriter(), "%s: ", label); err != nil {
				return "", err
			}
			b, err := term.ReadPassword(fd)
			if _, werr := fmt.Fprintln(a.promptWriter()); err == nil {
				err = werr
			}
			return string(b), err
		}
	}
	return a.prompt(label)
}
