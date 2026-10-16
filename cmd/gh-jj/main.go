// SPDX-License-Identifier: EUPL-1.2
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/wstein/sourcesteward/internal/cli"
)

var version = "dev"

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return cli.RunGHJJ(ctx, version, os.Args[1:], os.Stdout, os.Stderr)
}
