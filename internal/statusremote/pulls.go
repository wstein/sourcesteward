// SPDX-License-Identifier: EUPL-1.2

// Package statusremote reads the remote sources of the online `stw status` (Spec 037) and turns what
// they read into the inputs of the action model in internal/overview. It sends no request of its own:
// every read goes through a reader the caller built (the status reader of internal/hosting) and the
// cycles of the commands that already own the semantics, so the figures of `stw status` and of
// `stw pulls` cannot drift apart.
package statusremote

import (
	"strings"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Host is the one forge host of a run (Spec 037).
const Host = "github.com"

// Origin is what the local half knows of one repository of the workspace: its name, the URL of its
// `origin` (empty when it has none) and whether the local status of the repository ended `ok`.
type Origin struct {
	Name     string
	Remote   string
	Readable bool
}

// Target is one repository of the run. Slug is its OWNER/NAME on Host, empty when the repository
// is skipped; Skip is then the skip reason of the remote sources.
type Target struct {
	Name string
	Slug string
	Skip string
}

// Select derives the repository of each origin, in the order of the input. A repository that could
// not be read locally, has no usable origin, has one on another host or one whose path is not
// OWNER/NAME is skipped with its reason: none of its remote sources runs and no request is made for it.
func Select(origins []Origin) []Target {
	targets := make([]Target, len(origins))
	for i, o := range origins {
		targets[i] = Target{Name: o.Name}
		targets[i].Slug, targets[i].Skip = slugOf(o)
	}
	return targets
}

func slugOf(o Origin) (slug, skip string) {
	if !o.Readable {
		return "", overview.ReasonNotReadLocally
	}
	host, _, ok := watchcmd.SplitRemote(o.Remote)
	switch {
	case !ok:
		return "", overview.ReasonNotGitHub
	case !strings.EqualFold(host, Host):
		return "", overview.ReasonOtherHost
	}
	slug, ok = watchcmd.ParseRemote(o.Remote, Host)
	if !ok { // the host matches, so the path is what ParseRemote refused
		return "", overview.ReasonInvalidRepo
	}
	return slug, ""
}
