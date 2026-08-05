package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func (a *app) promptWriter() io.Writer {
	if a.jsonOut && a.stderr != nil {
		return a.stderr
	}
	return a.stdout
}

func (a *app) prompt(label string) (string, error) {
	if a.nonInteractive {
		return "", fmt.Errorf("%s required in non-interactive mode", label)
	}
	if _, err := fmt.Fprintf(a.promptWriter(), "%s: ", label); err != nil {
		return "", err
	}
	return a.readLine()
}

func (a *app) promptDefault(label, def string) (string, error) {
	if a.nonInteractive {
		if def == "" {
			return "", fmt.Errorf("%s required in non-interactive mode", label)
		}
		return def, nil
	}
	if def == "" {
		return a.prompt(label)
	}
	if _, err := fmt.Fprintf(a.promptWriter(), "%s [%s]: ", label, def); err != nil {
		return "", err
	}
	s, err := a.readLine()
	if err != nil {
		return "", err
	}
	if s == "" {
		return def, nil
	}
	return s, nil
}

func (a *app) readLine() (string, error) {
	if a.reader == nil {
		a.reader = bufio.NewReader(a.stdin)
	}
	s, err := a.reader.ReadString('\n')
	return strings.TrimSpace(s), err
}
