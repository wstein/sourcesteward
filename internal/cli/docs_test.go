// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestCommandDocumentationUsesCommandTreeWithoutExecution(t *testing.T) {
	docs := CommandReference()
	if !reflect.DeepEqual(docs, CommandReference()) {
		t.Fatal("nondeterministic reference metadata")
	}
	seen := map[string]CommandDocumentation{}
	for _, doc := range docs {
		seen[doc.Path] = doc
	}
	for _, name := range []string{"stw", "stw help", "stw inspect", "stw version", "stw workspace", "stw workspace plan", "stw workspace apply", "stw workspace handoff", "stw workspace retire-plan"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing command %q: %#v", name, docs)
		}
	}
	if !strings.Contains(seen["stw version"].Flags, "--json") {
		t.Fatal("version JSON flag missing")
	}
	// No execution is needed: the same command metadata supplies actual CLI help.
	for _, doc := range docs {
		var output strings.Builder
		args := strings.Fields(doc.Path)[1:]
		args = append(args, "--help")
		if status := Run(context.Background(), "dev", args, &output, io.Discard); status != 0 {
			t.Fatalf("help %q failed", doc.Path)
		}
		if !strings.Contains(output.String(), doc.Short) && !strings.Contains(output.String(), doc.Long) {
			t.Fatalf("reference drift for %q", doc.Path)
		}
	}
}
