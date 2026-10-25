// SPDX-License-Identifier: EUPL-1.2
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/wstein/sourcesteward/internal/cli"
)

var version = "dev"

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first SIGINT or SIGTERM restore the default behaviour, so a second one ends the process.
	go func() { <-ctx.Done(); stop() }()
	// The private native route of the build checkout comparison re-executes stw as "git" and
	// must be handled before any command parsing. It is not a public command: it applies only
	// with argv0 "git" and the private role variable, and a successful route replaces the process.
	if handled, status := cli.RunPrivateRoute(ctx); handled {
		return status
	}
	return cli.RunWithStdin(ctx, version, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
