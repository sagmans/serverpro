//go:build serverpro_e2e

package main

import (
	"fmt"
	"os"

	"github.com/sagmans/serverpro/internal/cli"
)

const e2eAPIURLEnv = "SERVERPRO_E2E_API_URL"

func main() {
	apiURL := os.Getenv(e2eAPIURLEnv)
	if apiURL == "" {
		fmt.Fprintf(os.Stderr, "%s required\n", e2eAPIURLEnv)
		os.Exit(2)
	}
	cmd, err := cli.NewE2E(apiURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
