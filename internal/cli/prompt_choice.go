package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/filedescriptor"
	"golang.org/x/term"
)

type choice struct {
	Value       string
	Description string
}

func (a *app) promptChoice(label, def string, choices []choice) (string, error) {
	if a.nonInteractive || len(choices) == 0 {
		return a.promptDefault(label, def)
	}
	if a.selectChoice != nil {
		selected, ok, err := a.selectChoice(label, def, choices)
		if err != nil || ok {
			return selected, err
		}
	}
	if !a.interactiveTerminal() {
		return a.promptDefault(label, def)
	}
	if selected, ok := a.fzfChoice(label, choices); ok {
		return selected, nil
	}
	for i, option := range choices {
		marker := " "
		if option.Value == def {
			marker = "*"
		}
		if _, err := fmt.Fprintf(a.promptWriter(), "%s %2d) %-14s %s\n", marker, i+1, option.Value, option.Description); err != nil {
			return "", err
		}
	}
	selected, err := a.promptDefault(label, def)
	if err != nil {
		return "", err
	}
	return choiceValueFromAnswer(selected, choices), nil
}

func choiceValueFromAnswer(selected string, choices []choice) string {
	if n, err := strconv.Atoi(selected); err == nil && n >= 1 && n <= len(choices) {
		return choices[n-1].Value
	}
	return selected
}

func (a *app) interactiveTerminal() bool {
	in, inOK := a.stdin.(*os.File)
	out, outOK := a.stdout.(*os.File)
	if !inOK || !outOK {
		return false
	}
	inFD, err := filedescriptor.Int(in)
	if err != nil {
		return false
	}
	outFD, err := filedescriptor.Int(out)
	if err != nil {
		return false
	}
	return term.IsTerminal(inFD) && term.IsTerminal(outFD)
}

func (a *app) fzfChoice(label string, choices []choice) (string, bool) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", false
	}
	var input strings.Builder
	for _, option := range choices {
		_, _ = fmt.Fprintf(&input, "%s\t%s\n", option.Value, option.Description)
	}
	cmd := exec.Command("fzf", "--prompt", label+": ", "--height", "40%", "--with-nth", "1..")
	cmd.Stdin = strings.NewReader(input.String())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", false
	}
	return strings.Fields(line)[0], true
}
