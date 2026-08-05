package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/sagmans/serverpro/internal/state"
)

// loadState adds target context to missing explicit state files.
func loadState(path, namespace, server string) (state.State, error) {
	st, err := state.Load(path)
	if err == nil {
		return st, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if namespace != "" {
			return state.State{}, fmt.Errorf("no local state for namespace %q server %q at %s", namespace, targetServer(server), path)
		}
		return state.State{}, fmt.Errorf("no local state at %s", path)
	}
	return state.State{}, err
}
