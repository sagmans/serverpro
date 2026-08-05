package doctor

import (
	"context"
	"testing"

	"github.com/assagman/serverpro/internal/bootstraptools"
)

type remoteCall struct {
	out string
	err error
}

type scriptedRemote struct {
	commands  []string
	inputs    []string
	responses map[string][]remoteCall
}

func (f *scriptedRemote) Run(_ context.Context, user, host, script string) (string, error) {
	f.commands = append(f.commands, script)
	calls := f.responses[script]
	if len(calls) == 0 {
		return "ok", nil
	}
	call := calls[0]
	f.responses[script] = calls[1:]
	return call.out, call.err
}

func (f *scriptedRemote) RunWithInput(_ context.Context, user, host, script, input string) (string, error) {
	f.commands = append(f.commands, script)
	f.inputs = append(f.inputs, input)
	calls := f.responses[script]
	if len(calls) == 0 {
		return "ok", nil
	}
	call := calls[0]
	f.responses[script] = calls[1:]
	return call.out, call.err
}

func remoteToolCheckByName(t *testing.T, checks []bootstraptools.Check, name string) bootstraptools.Check {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing bootstrap tool check %q: %+v", name, checks)
	return bootstraptools.Check{}
}
