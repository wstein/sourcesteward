// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func TestRepoViewGivesTheRowsStatusAndCountsOfOneRepository(t *testing.T) {
	f := newForge()
	f.add(okPR("a/x", 1))
	d := okPR("a/x", 2)
	d.draft = true
	f.add(d)
	f.add(okPR("b/y", 3))
	f.listErr["c/z"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "down"}
	res := run(t, f, Options{Repos: []string{"c/z", "b/y", "a/x"}})
	v, ok := res.Repo("A/X")
	if !ok || v.Status.Repo != "a/x" || len(v.Rows) != 2 || v.Summary.Total != 2 || v.Summary.Ready != 1 || v.Summary.Draft != 1 {
		t.Fatalf("%+v %v", v, ok)
	}
	if v, ok := res.Repo("b/y"); !ok || len(v.Rows) != 1 || v.Rows[0].Number != 3 {
		t.Fatalf("%+v", v)
	}
	if v, ok := res.Repo("c/z"); !ok || len(v.Rows) != 0 || v.Status.Error == "" {
		t.Fatalf("%+v", v)
	}
	if _, ok := res.Repo("d/w"); ok {
		t.Fatal("unknown repository")
	}
}
