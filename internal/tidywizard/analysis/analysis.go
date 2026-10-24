// SPDX-License-Identifier: EUPL-1.2

// Package analysis prepares and runs the read-only analysis of the tidy wizard (Spec 041 T019a):
// it finds the workspace and its members, derives the remote and the base of every repository and
// the state directories, and classifies the repositories through the adapter (four at a time,
// through internal/fanout). It never writes, never fetches and never touches the network. Discovery
// is Git only (stwsync.Discover): outside a Git workspace Prepare fails with a usage error.
package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

// Options are the wizard-layer inputs of the analysis.
type Options struct {
	// Workspace is --workspace: where discovery starts; empty means CWD.
	Workspace string
	// CWD is the working directory discovery starts from without --workspace.
	CWD string
	// Members is --member (repeatable); empty means every initialized member.
	Members []string
	// MemberBases is --member-base NAME=REF (repeatable).
	MemberBases []string
	// StateDirs is --state-dir (repeatable); the first valid store is the plan store.
	StateDirs []string
	// Online allows the contract-3 authenticated reads of tracking evidence.
	Online bool
}

// Deps are the seams of the preparation; DefaultDeps is the production set.
type Deps struct {
	Discover        func(context.Context, string) (stwsync.Workspace, error)
	Refs            func(context.Context, string) (stwsync.RepoRefs, error)
	DefaultStateDir func() (string, error)
	// StateEnv is $STW_STATE_DIR.
	StateEnv string
}

// DefaultDeps reads the real workspace, the real references, and the shared state directory.
func DefaultDeps() Deps {
	return Deps{Discover: stwsync.Discover, Refs: stwsync.LocalRefs, DefaultStateDir: derive.DefaultStateDir, StateEnv: derive.EnvStateDir()}
}

// UsageError is a usage error of the wizard layer (exit 2): Code is the reason code.
type UsageError struct{ Code, Message string }

func (e *UsageError) Error() string { return e.Code + ": " + e.Message }

// Prepared is everything the analysis and the overview need.
type Prepared struct {
	Workspace string
	Derived   []derive.Derived
	Specs     []tidywizard.RepoSpec
	Meta      map[string]render.RepoMeta
	// Names are the repositories to analyse, in member order.
	Names   []string
	Notices []string
	Stores  []derive.Store
	// PlanStore is the first valid state directory, empty when there is none.
	PlanStore string
}

// Prepare discovers the workspace and derives the defaults. It reads the references of each member
// (offline) and nothing else.
func Prepare(ctx context.Context, o Options, d Deps) (Prepared, error) {
	start := o.Workspace
	if start == "" {
		start = o.CWD
	}
	ws, err := d.Discover(ctx, start)
	if err != nil {
		return Prepared{}, &UsageError{Code: "workspace-not-found", Message: "no Git workspace was found from " + start + " (" + err.Error() + "); give --workspace PATH, or use the expert layer"}
	}
	wsDerived := derive.Workspace(o.Workspace, ws.Root)
	p := Prepared{Workspace: ws.Root, Derived: []derive.Derived{wsDerived.Derived}, Meta: map[string]render.RepoMeta{}, Specs: []tidywizard.RepoSpec{}, Names: []string{}, Notices: []string{}}

	byName := map[string]stwsync.Member{}
	var discovered []string
	for _, m := range ws.Members {
		byName[m.Name] = m
		discovered = append(discovered, m.Name)
	}
	// The member set is the workspace repository first and then the members once each; a
	// workspace without another member says so (Spec 041, derivation of defaults).
	order, none := derive.Members(discovered)
	if none != "" {
		p.Notices = append(p.Notices, none)
	}
	selected := order
	if len(o.Members) > 0 {
		want := map[string]bool{}
		for _, name := range o.Members {
			if _, ok := byName[name]; !ok {
				return Prepared{}, &UsageError{Code: "unknown-member", Message: fmt.Sprintf("--member %q is not a member of the workspace", name)}
			}
			want[name] = true
		}
		selected = nil
		for _, name := range order {
			if want[name] {
				selected = append(selected, name)
			}
		}
	}
	overrides := map[string]string{}
	for _, v := range o.MemberBases {
		name, ref, ok := strings.Cut(v, "=")
		if !ok || name == "" || ref == "" {
			return Prepared{}, &UsageError{Code: "invalid-member-base", Message: fmt.Sprintf("--member-base needs NAME=REF, got %q", v)}
		}
		if _, dup := overrides[name]; dup {
			return Prepared{}, &UsageError{Code: "invalid-member-base", Message: fmt.Sprintf("--member-base names %q twice", name)}
		}
		if _, ok := byName[name]; !ok {
			return Prepared{}, &UsageError{Code: "unknown-member", Message: fmt.Sprintf("--member-base names %q, which is not a member of the workspace", name)}
		}
		overrides[name] = ref
	}

	for _, name := range selected {
		m := byName[name]
		if !m.Initialized {
			// The existing code of the repository inspection (Spec 023).
			p.Notices = append(p.Notices, "submodule-uninitialized: "+name)
			continue
		}
		spec := tidywizard.RepoSpec{Name: name, Path: m.Path}
		meta := render.RepoMeta{Path: m.Path, Backend: "git"}
		refs, err := d.Refs(ctx, m.Path)
		switch {
		case err != nil:
			// Not an ambiguity: the repository is reported as an error with the reason.
			spec.Error = err.Error()
		case len(refs.Remotes) == 0:
			// Without a remote there is no symbolic HEAD to derive the base from.
			spec.Skip = "base-ambiguous"
		default:
			remote := derive.Remote(name, refs.Remotes)
			if remote.Ambiguity != nil {
				spec.Skip = remote.Ambiguity.Code
				break
			}
			p.Derived = append(p.Derived, remote.Derived)
			chosen := remote.Derived.Value
			facts := derive.RepoFacts{Name: name, Backend: "git", RemoteHead: map[string]string{}, Refs: refs.Refs}
			facts.RemoteHead[chosen] = localHead(refs, chosen)
			base := derive.Base(facts, chosen, overrides[name])
			if base.Ambiguity != nil {
				spec.Skip = base.Ambiguity.Code
				break
			}
			meta.Remote = chosen
			if o.Online {
				// The observer takes a remote exactly when it reads online.
				spec.Remote, spec.Online = chosen, true
			}
			p.Derived = append(p.Derived, base.Derived)
			spec.Base, meta.Base = base.Derived.Value, base.Derived.Value
		}
		p.Specs = append(p.Specs, spec)
		p.Meta[name] = meta
		p.Names = append(p.Names, name)
	}

	def := ""
	if d.DefaultStateDir != nil {
		def, _ = d.DefaultStateDir()
	}
	p.Stores = derive.Stores(o.StateDirs, d.StateEnv, def)
	p.Derived = append(p.Derived, derive.StoresDerived(p.Stores))
	if s, ok := derive.PlanStore(p.Stores); ok {
		p.PlanStore = s.Path
	}
	return p, nil
}

// localHead maps the symbolic HEAD of a remote to the local branch of the same name when that
// branch exists, else to the remote-tracking reference itself (which exists locally, the wizard
// never fetches); empty when the remote has no symbolic HEAD.
func localHead(r stwsync.RepoRefs, remote string) string {
	target := r.Heads[remote]
	if target == "" {
		return ""
	}
	if branch, ok := strings.CutPrefix(target, "refs/remotes/"+remote+"/"); ok {
		if local := "refs/heads/" + branch; r.Refs[local] {
			return local
		}
	}
	return target
}

// Run classifies the prepared repositories through the real adapter, four at a time.
func Run(ctx context.Context, p Prepared, cfg tidywizard.Config) tidywizard.Result {
	return tidywizard.Classify(ctx, tidywizard.NewAdapter(p.Specs, p.Stores), p.Names, cfg)
}
