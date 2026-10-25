// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

type markerReader struct{ strings.Reader }

// The command line entry point hands the process's standard input to the command, so that the
// terminal facts are those of the real descriptors: the empty reader of Run (kept for callers
// without a terminal) must not be what the command sees.
func TestRunWithStdinForwardsTheReaderAndTheWriterToTheTerminalFacts(t *testing.T) {
	in := &markerReader{*strings.NewReader("")}
	var gotIn, gotOut any
	old := tidyTerminalFacts
	t.Cleanup(func() { tidyTerminalFacts = old })
	tidyTerminalFacts = func(i, o any) (bool, bool) { gotIn, gotOut = i, o; return false, false }
	var out, diag bytes.Buffer
	_ = RunWithStdin(context.Background(), "test", []string{"tidy", "apply", setID, "--yes", "--state-dir", "/s"}, in, &out, &diag)
	if gotIn != any(in) || gotOut != any(&out) {
		t.Errorf("facts asked about %T and %T, want the given reader and writer", gotIn, gotOut)
	}
	// Run keeps working without a terminal: an empty input that is not the caller's.
	gotIn = nil
	_ = Run(context.Background(), "test", []string{"tidy", "apply", setID, "--yes", "--state-dir", "/s"}, &out, &diag)
	if gotIn == nil || gotIn == any(in) {
		t.Errorf("Run asked about %T", gotIn)
	}
}
