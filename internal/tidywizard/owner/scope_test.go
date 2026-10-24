// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"errors"
	"reflect"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
)

func entries() []Entry {
	return []Entry{
		{"a", Owner{"claude", SourceTaskRecord, ScopeOwn}},
		{"b", Owner{"claude", SourcePathConvention, ScopeOwn}},
		{"c", Owner{"codex", SourcePathConvention, ScopeOther}},
		{"d", Owner{"codex", SourceTaskRecord, ScopeOther}},
		{"e", Owner{"", SourceUnknown, ScopeUnknown}},
		{"f", Owner{"author:x", SourceCommitAuthor, ScopeUnknown}},
	}
}

func ids(es []Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func TestPlannableHumanKeepsEverything(t *testing.T) {
	got := ids(Plannable(entries(), Evidence{Mode: ModeHuman}))
	if want := []string{"a", "b", "c", "d", "e", "f"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("human plannable = %v, want %v", got, want)
	}
}

func TestSaveInAgentModeExcludesForeignItems(t *testing.T) {
	got := ids(Plannable(entries(), Evidence{Mode: ModeAgent}))
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("agent plannable = %v, want %v (no other, no unknown, own only via task-record)", got, want)
	}
}

func TestOwnInAgentModeRequiresTaskRecord(t *testing.T) {
	// Even a mislabelled entry (own through a path guess) is not plannable by an agent.
	es := []Entry{{"x", Owner{"claude", SourcePathConvention, ScopeOwn}}, {"y", Owner{"claude", SourceCommitAuthor, ScopeOwn}}}
	if got := Plannable(es, Evidence{Mode: ModeAgent}); len(got) != 0 {
		t.Fatalf("agent planned %v", ids(got))
	}
}

func TestViewFilters(t *testing.T) {
	cases := []struct {
		name string
		ev   Evidence
		want []string
	}{
		{"human no flag", Evidence{Mode: ModeHuman}, []string{"a", "b", "c", "d", "e", "f"}},
		{"mine", Evidence{Mode: ModeAgent, Mine: true}, []string{"a"}},
		{"agent default shows task-record own only", Evidence{Mode: ModeAgent}, []string{"a"}},
		{"human mine keeps every own item", Evidence{Mode: ModeHuman, Mine: true}, []string{"a", "b"}},
		{"by-owner other label is a read-only view", Evidence{Mode: ModeAgent, ByOwner: []string{"codex"}}, []string{"c", "d"}},
		{"by-owner repeated", Evidence{Mode: ModeAgent, ByOwner: []string{"codex", "claude"}}, []string{"a", "b", "c", "d"}},
		{"mine and by-owner intersect", Evidence{Mode: ModeAgent, Mine: true, ByOwner: []string{"codex"}}, []string{}},
		{"agent mine with own label shows task-record own only", Evidence{Mode: ModeAgent, Mine: true, ByOwner: []string{"claude"}}, []string{"a"}},
		{"human by-owner is a view of that label", Evidence{Mode: ModeHuman, ByOwner: []string{"codex"}}, []string{"c", "d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ids(View(entries(), c.ev)); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("view = %v, want %v", got, c.want)
			}
		})
	}
}

func TestByOwnerOfAnotherLabelIsNotPlannable(t *testing.T) {
	ev := Evidence{Mode: ModeAgent, ByOwner: []string{"codex"}}
	if got := Plannable(View(entries(), ev), ev); len(got) != 0 {
		t.Fatalf("agent planned a read-only view: %v", ids(got))
	}
}

func TestHumanByOwnerCanPlanForeignItems(t *testing.T) {
	ev := Evidence{Mode: ModeHuman, ByOwner: []string{"codex"}}
	if got := ids(Plannable(View(entries(), ev), ev)); !reflect.DeepEqual(got, []string{"c", "d"}) {
		t.Fatalf("human by-owner plannable = %v, want [c d]", got)
	}
}

func TestAuthorizeScopesHumanOnly(t *testing.T) {
	got, err := Authorize(Evidence{Mode: ModeHuman}, ScopeUnknown, ScopeOther, ScopeOther)
	if err != nil || !reflect.DeepEqual(got, []Scope{ScopeOther, ScopeUnknown}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err = Authorize(Evidence{Mode: ModeAgent}, ScopeOther); !errors.Is(err, ErrAgentMode) {
		t.Fatalf("agent authorize err = %v, want ErrAgentMode", err)
	}
	if _, err = Authorize(Evidence{Mode: ModeHuman}, ScopeOwn); err == nil {
		t.Fatal("own is not a scope that needs authorization")
	}
	if _, err = Authorize(Evidence{Mode: ModeHuman}, Scope("everything")); err == nil {
		t.Fatal("unknown scope value accepted")
	}
}

func TestPlanIDCanonicalisesNilAndEmpty(t *testing.T) {
	a := PlanContent{Mode: Evidence{Mode: ModeHuman}}
	b := PlanContent{SubPlans: []string{}, Derived: []derive.Derived{}, Selected: []string{}, AuthorizedScopes: []Scope{}, Mode: Evidence{Mode: ModeHuman, Markers: []string{}, ByOwner: []string{}}}
	if PlanID(a) != PlanID(b) {
		t.Fatal("nil and empty content give different plan ids")
	}
	if PlanID(PlanContent{Mode: Evidence{Mode: ModeHuman, Markers: []string{"A"}}}) == PlanID(a) {
		t.Fatal("markers are not part of the plan id")
	}
}

func TestAuthorizedScopesArePartOfThePlanID(t *testing.T) {
	base := PlanContent{
		SubPlans: []string{"p2", "p1"},
		Derived:  []derive.Derived{{Name: "remote:r", Value: "origin", Source: "origin"}},
		Selected: []string{"s2", "s1"},
		Mode:     Evidence{Mode: ModeHuman, Markers: []string{}, ByOwner: []string{}},
	}
	id := PlanID(base)
	if len(id) != 12 {
		t.Fatalf("plan id %q must be 12 hex characters", id)
	}
	if PlanID(base) != id {
		t.Fatal("plan id is not deterministic")
	}
	reordered := base
	reordered.SubPlans = []string{"p1", "p2"}
	reordered.Selected = []string{"s1", "s2"}
	if PlanID(reordered) != id {
		t.Fatal("plan id depends on the order of sub-plans or selection")
	}
	// Editing authorized_scopes changes the id.
	authorized := base
	authorized.AuthorizedScopes = []Scope{ScopeOther}
	if PlanID(authorized) == id {
		t.Fatal("authorized_scopes is not part of the plan id")
	}
	both := base
	both.AuthorizedScopes = []Scope{ScopeUnknown, ScopeOther}
	if PlanID(both) == PlanID(authorized) {
		t.Fatal("authorized_scopes content is not part of the plan id")
	}
	swapped := both
	swapped.AuthorizedScopes = []Scope{ScopeOther, ScopeUnknown}
	if PlanID(swapped) != PlanID(both) {
		t.Fatal("authorized_scopes order changes the id")
	}
	// The other bound inputs change the id too.
	changed := base
	changed.Mode.Mode = ModeAgent
	if PlanID(changed) == id {
		t.Fatal("mode evidence is not part of the plan id")
	}
	changed = base
	changed.Selected = []string{"s1"}
	if PlanID(changed) == id {
		t.Fatal("selection is not part of the plan id")
	}
}
