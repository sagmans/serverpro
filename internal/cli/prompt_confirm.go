package cli

import (
	"errors"
	"fmt"
	"strings"
)

func (a *app) confirm(msg string) error {
	ok, err := a.confirmDefault(msg, false)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return errors.New("cancelled")
}

func (a *app) confirmDefault(msg string, yesDefault bool) (bool, error) {
	if a.nonInteractive {
		return false, errors.New("--yes required for non-interactive confirmation")
	}
	prompt := "[y/N]"
	if yesDefault {
		prompt = "[Y/n]"
	}
	if _, err := fmt.Fprintf(a.promptWriter(), "%s %s: ", msg, prompt); err != nil {
		return false, err
	}
	s, err := a.readLine()
	if err != nil {
		return false, err
	}
	if s == "" {
		return yesDefault, nil
	}
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes"), nil
}
