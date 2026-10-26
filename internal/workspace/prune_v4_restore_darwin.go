// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"golang.org/x/sys/unix"
	"os"
)

func publishPruneV4Restore(source *os.Root, from string, dest *os.Root, to string) error {
	a, e := source.Open(".")
	if e != nil {
		return e
	}
	defer a.Close()
	b, e := dest.Open(".")
	if e != nil {
		return e
	}
	defer b.Close()
	return unix.RenameatxNp(int(a.Fd()), from, int(b.Fd()), to, unix.RENAME_EXCL)
}
