// Command genbootstrapwrapper prints the generated manual bootstrap wrapper
// (scripts/serverpro-bootstrap-tools.sh) so the wrapper is regenerated from
// the single pin manifest in internal/bootstraptools instead of maintained by
// hand. Usage: make gen-bootstrap-wrapper.
package main

import (
	"fmt"

	"github.com/assagman/serverpro/internal/bootstraptools"
)

func main() {
	fmt.Print(bootstraptools.WrapperScript())
}
