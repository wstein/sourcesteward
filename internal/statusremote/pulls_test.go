// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"reflect"
	"testing"

	"github.com/wstein/sourcesteward/internal/overview"
)

func TestSelectDerivesTheRepositoryFromOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin Origin
		slug   string
		skip   string
	}{
		{"https", Origin{Name: "a", Remote: "https://github.com/wstein/a.git", Readable: true}, "wstein/a", ""},
		{"scp", Origin{Name: "a", Remote: "git@github.com:wstein/a", Readable: true}, "wstein/a", ""},
		{"ssh", Origin{Name: "a", Remote: "ssh://git@github.com/wstein/a.git", Readable: true}, "wstein/a", ""},
		{"upper-case host", Origin{Name: "a", Remote: "https://GitHub.com/wstein/a", Readable: true}, "wstein/a", ""},
		{"not read locally wins", Origin{Name: "a", Remote: "https://github.com/wstein/a", Readable: false}, "", overview.ReasonNotReadLocally},
		{"no origin", Origin{Name: "a", Readable: true}, "", overview.ReasonNotGitHub},
		{"local path", Origin{Name: "a", Remote: "/srv/git/a.git", Readable: true}, "", overview.ReasonNotGitHub},
		{"unsupported scheme", Origin{Name: "a", Remote: "file:///github.com/wstein/a", Readable: true}, "", overview.ReasonNotGitHub},
		{"other host", Origin{Name: "a", Remote: "git@gitlab.com:wstein/a.git", Readable: true}, "", overview.ReasonOtherHost},
		{"look-alike host", Origin{Name: "a", Remote: "https://github.com.evil.example/wstein/a", Readable: true}, "", overview.ReasonOtherHost},
		{"too many segments", Origin{Name: "a", Remote: "https://github.com/wstein/a/b", Readable: true}, "", overview.ReasonInvalidRepo},
		{"owner only", Origin{Name: "a", Remote: "https://github.com/wstein", Readable: true}, "", overview.ReasonInvalidRepo},
		{"dot segment", Origin{Name: "a", Remote: "https://github.com/wstein/../a", Readable: true}, "", overview.ReasonInvalidRepo},
	}
	for _, c := range cases {
		got := Select([]Origin{c.origin})
		want := []Target{{Name: "a", Slug: c.slug, Skip: c.skip}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, want)
		}
	}
}

func TestSelectKeepsTheOrderOfTheInput(t *testing.T) {
	got := Select([]Origin{
		{Name: "root", Remote: "https://github.com/wstein/anchorage", Readable: true},
		{Name: "b", Remote: "https://gitlab.com/x/b", Readable: true},
		{Name: "a", Remote: "https://github.com/wstein/a", Readable: true},
	})
	var names []string
	for _, tg := range got {
		names = append(names, tg.Name)
	}
	if !reflect.DeepEqual(names, []string{"root", "b", "a"}) {
		t.Fatalf("order %v", names)
	}
}
