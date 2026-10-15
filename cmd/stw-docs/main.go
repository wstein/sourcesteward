// SPDX-License-Identifier: EUPL-1.2
// stw-docs exports build-time Cobra metadata without executing CLI operations.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/wstein/sourcesteward/internal/cli"
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(cli.CommandReference()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
