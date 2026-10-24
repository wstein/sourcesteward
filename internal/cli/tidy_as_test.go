// SPDX-License-Identifier: EUPL-1.2

package cli

import (
	"testing"
)

// Spec 041 T018a: the wizard has no --as. Only `restore` keeps its meaning of --as (the full
// refs/heads/ name a restored branch gets); every other tidy command refuses it as a usage error.
func TestTidyAsExistsOnlyOnRestore(t *testing.T) {
	for name, args := range map[string][]string{
		"tidy":            {"--as", "x"},
		"tidy plan":       {"plan", "--as", "x"},
		"tidy plan PATH":  {"plan", "/r", "--as", "x"},
		"tidy apply":      {"apply", "--as", "x"},
		"tidy apply id":   {"apply", setID, "--as", "x"},
		"tidy history":    {"history", "--as", "x"},
		"tidy retirement": {"retire", "--as", "x"},
	} {
		got := fakeSet(t, okSet(), false, false)
		_, err := runTidy(t, "", args...)
		if !isUsage(err) || len(*got) != 0 {
			t.Errorf("%s: err %v calls %d", name, err, len(*got))
		}
	}
	root := tidyCommand()
	for _, c := range root.Commands() {
		has := c.Flags().Lookup("as") != nil
		if want := c.Name() == "restore"; has != want {
			t.Errorf("%s: --as registered=%v, want %v", c.Name(), has, want)
		}
	}
	if root.Flags().Lookup("as") != nil || root.PersistentFlags().Lookup("as") != nil {
		t.Error("the tidy family itself must not register --as")
	}
}
