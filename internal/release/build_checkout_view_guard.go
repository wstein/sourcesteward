// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// buildCheckoutView is the production consumer of the private checkout view. It freezes
// the metadata of the build checkout (descriptor-relative, no-follow capture of the .git
// anchor, gitdir, common dir, configuration and index; nested gitlink checkouts included),
// compares the tracked tree with the requested commit through that view, and revalidates
// every binding after the project build. It holds the single capture lease until Close,
// which also removes the private state, so a later build cannot start while it lives.
type buildCheckoutView struct {
	root    string
	graph   *buildViewGraph
	private string
	release func()
}

// buildViewSupervisor is the native runner of the view; a test substitutes it.
var buildViewSupervisor buildViewNative = superviseBuildViewNative

func openBuildCheckoutView(ctx context.Context, root, commit string, inherited []string) (_ *buildCheckoutView, retErr error) {
	// Conversion helpers (filter.*.clean/process, textconv) are screened first, exactly as
	// before the view existed: a configured helper refuses before any native comparison.
	if err := checkBuildCheckoutHelpers(ctx, root); err != nil {
		return nil, err
	}
	realGit, err := resolveBuildViewGit()
	if err != nil {
		return nil, errors.New("cannot locate native Git for the checkout view")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, errors.New("cannot locate the stw executable for the checkout view")
	}
	if executable, err = filepath.Abs(executable); err != nil {
		return nil, err
	}
	release, err := acquireBuildViewLease(ctx)
	if err != nil {
		return nil, err
	}
	v := &buildCheckoutView{root: root, release: release}
	// Not keyed on retErr alone: a panic below must also remove the private state and
	// release the lease before it propagates.
	opened := false
	defer func() {
		if !opened {
			_ = v.Close()
		}
	}()
	v.private, err = os.MkdirTemp("", "stw-checkout-view-")
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(v.private, 0700); err != nil {
		return nil, err
	}
	v.graph, err = prepareBuildViewGraph(ctx, buildViewGraphOptions{Root: root, Commit: commit, Private: v.private, RealGit: realGit, Executable: executable, Inherited: inherited}, buildViewSupervisor)
	if err != nil {
		return nil, err
	}
	opened = true
	return v, nil
}

// check screens conversion helpers again (a build may add one) and compares the tracked
// tree with the requested commit through the frozen view, refusing when any bound
// metadata, the captured configuration or HEAD changed since the capture.
func (v *buildCheckoutView) check(ctx context.Context) error {
	if err := checkBuildCheckoutHelpers(ctx, v.root); err != nil {
		return err
	}
	return v.graph.check(ctx)
}

func (v *buildCheckoutView) Close() error {
	var err error
	if v.graph != nil {
		err = v.graph.Close()
		v.graph = nil
	}
	if v.private != "" {
		err = errors.Join(err, os.RemoveAll(v.private))
		v.private = ""
	}
	if v.release != nil {
		v.release()
		v.release = nil
	}
	return err
}

// superviseBuildViewNative is the production native runner: the supervised process group.
func superviseBuildViewNative(ctx context.Context, c buildViewCommand, out io.Writer) (int, []byte, error) {
	return runBuildViewNative(ctx, c, out)
}
