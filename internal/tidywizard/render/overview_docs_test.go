// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"os"
	"strings"
	"testing"
)

// The screens of docs/tidy.md are the recorded output, byte for byte: the tests above pin the
// goldens, this one pins the documentation to them (examples are pasted, never written by hand).
func TestTidyDocsShowTheRecordedScreens(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/tidy.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"overview-real-activity-old.txt.golden", "retain-real.txt.golden"} {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		want := "```text\n" + strings.TrimRight(string(b), "\n") + "\n```\n"
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/tidy.md does not contain the recorded screen %s:\n%s", name, want)
		}
	}
}
