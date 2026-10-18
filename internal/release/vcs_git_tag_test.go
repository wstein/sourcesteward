// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReviewCorrectionTagTypeAndCommitAreOneObservation(t *testing.T) {
	d, c, _ := recordCommandFixture(t)
	git(t, d, "tag", "-a", "v0.2.0", "-m", "annotated")
	write(t, d, "other.txt", []byte("second commit"))
	second := commitAll(t, d)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	marker := filepath.Join(bin, "swapped")
	body := "#!/bin/sh\ncase \"$*\" in\n  *\"cat-file -t \"*)\n    '" + real + "' \"$@\"\n    '" + real + "' update-ref refs/tags/v0.2.0 '" + second + "'\n    touch '" + marker + "'\n    exit 0;;\nesac\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, annotated, err := resolveGitTag(context.Background(), d, "v0.2.0")
	if _, e := os.Stat(marker); e != nil {
		t.Fatal("tag swap did not execute", e)
	}
	if err != nil || !annotated || got != c {
		t.Fatalf("claimed annotated tag for never-annotated second commit after split reads: old=%s second=%s got=%s", c, second, got)
	}
}
