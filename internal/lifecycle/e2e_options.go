//go:build serverpro_e2e

package lifecycle

import (
	"time"

	"github.com/sagmans/serverpro/internal/state"
)

// ConfigureE2E keeps checkpoint fault injection outside production builds.
func ConfigureE2E(opt Options, now func() time.Time, save func(string, state.State) error) Options {
	opt.Now = now
	opt.saveState = save
	return opt
}
