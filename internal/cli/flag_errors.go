package cli

import "fmt"

// requiredFlagError renders one shared shape for every missing required flag
// so callers get the exact flag and command path to pass or fix.
func requiredFlagError(commandPath, name, shorthand string) error {
	if shorthand != "" {
		return fmt.Errorf("--%s/-%s is required for %q", name, shorthand, commandPath)
	}
	return fmt.Errorf("--%s is required for %q", name, commandPath)
}
