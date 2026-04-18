package main

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
)

// requireFlags returns an error listing every flag whose resolved value is
// empty. It collects all missing values into a single error so the operator
// can see everything they need to fix in one run, rather than playing
// whack-a-mole with one missing env at a time.
func requireFlags(ctx *cli.Context, names ...string) error {
	var missing []string
	for _, n := range names {
		if strings.TrimSpace(ctx.String(n)) == "" {
			missing = append(missing, "--"+n)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"missing required flag(s): %s (see `--help` for the bound env vars)",
		strings.Join(missing, ", "),
	)
}
