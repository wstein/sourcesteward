// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type buildViewNative func(context.Context, buildViewCommand, io.Writer) (int, []byte, error)
type buildViewGraphOptions struct {
	Root, Commit, Private, RealGit, Executable string
	Inherited                                  []string
}
type buildViewRoot struct {
	Identity, GitdirIdentity, CommonIdentity                        buildViewIdentity
	Anchor, CommonPointer                                           buildViewMetadataSnapshot
	Worktree, Gitdir, Common, Objects, PrivateGitdir, PrivateCommon buildViewPath
	Head                                                            string
	Names, Values                                                   buildViewPath
	hashBytes                                                       int
}
type buildViewGraph struct {
	options  buildViewGraphOptions
	env      []string
	roots    []buildViewRoot
	bindings []*buildViewBinding
	native   buildViewNative
	seen     map[string]bool
	manifest string
	route    string
	scratch  string
	sealed   map[string]buildViewSealEntry
	// indexSeal holds, per root, the digest of the captured index's semantic content.
	indexSeal []([32]byte)
}

// Native output is staged on private disk, so it is bounded: a setup answer like the
// historical 64 KiB Git runner cap, listings by a generous total that still stops a
// runaway repository. Gitlink recursion is bounded by the number of captured checkouts.
var (
	buildViewQueryLimit   int64 = 64 << 10
	buildViewListingLimit int64 = 256 << 20
	buildViewMaxRoots           = 256
)

type buildViewLimitWriter struct {
	w    io.Writer
	left int64
}

func (l *buildViewLimitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, fmt.Errorf("%w: native checkout output exceeds limit", errReleaseLimit)
	}
	l.left -= int64(len(p))
	return l.w.Write(p)
}

var buildViewGitPrefix = []string{"--no-pager", "--no-replace-objects", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "hook.post-index-change.enabled=false"}

func prepareBuildViewGraph(ctx context.Context, o buildViewGraphOptions, native buildViewNative) (*buildViewGraph, error) {
	g := &buildViewGraph{options: o, native: native, seen: map[string]bool{}}
	// Close the bindings on every exit that does not return the graph, a panic included.
	prepared := false
	defer func() {
		if !prepared {
			_ = g.Close()
		}
	}()
	g.env = buildViewEnvironment(o.Inherited, o.RealGit, o.Private)
	if !filepath.IsAbs(o.Private) || !filepath.IsAbs(o.RealGit) || !filepath.IsAbs(o.Executable) || !IsFullCommitID(o.Commit) {
		return nil, errors.New("invalid private checkout preparation context")
	}
	g.scratch = filepath.Join(o.Private, buildViewScratch)
	if err := os.Mkdir(g.scratch, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(g.scratch, 0700); err != nil {
		return nil, err
	}
	if err := g.captureRoot(ctx, o.Root, o.Commit); err != nil {
		return nil, err
	}
	if err := g.validate(ctx); err != nil {
		return nil, err
	}
	if err := g.writeManifest(); err != nil {
		return nil, err
	}
	// Everything the native comparison trusts now exists. Native results that are only
	// read back by the job move to the scratch directory from here on.
	sealed, err := sealBuildViewPrivate(o.Private)
	if err != nil {
		return nil, err
	}
	g.sealed = sealed
	prepared = true
	return g, nil
}
func (g *buildViewGraph) Close() error {
	var err error
	for i := len(g.bindings) - 1; i >= 0; i-- {
		err = errors.Join(err, g.bindings[i].Close())
	}
	g.bindings = nil
	return err
}
func (g *buildViewGraph) bind(ctx context.Context, path string, raw bool) (*buildViewBinding, error) {
	var b *buildViewBinding
	var err error
	if raw {
		b, err = captureBuildViewMetadata(ctx, path, nil)
	} else {
		b, err = captureBuildViewPath(ctx, path, nil)
	}
	if err != nil {
		return nil, errors.New("cannot bind checkout metadata")
	}
	g.bindings = append(g.bindings, b)
	return b, nil
}
func (g *buildViewGraph) read(ctx context.Context, root string, env []string, limit int64, args ...string) (*os.File, int, error) {
	dir := g.options.Private
	if g.sealed != nil {
		dir = g.scratch
	}
	f, err := os.CreateTemp(dir, "native-")
	if err != nil {
		return nil, -1, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, -1, err
	}
	status, diagnostic, err := g.native(ctx, buildViewCommand{Executable: g.options.RealGit, Dir: root, Env: env, Args: append(append([]string{}, buildViewGitPrefix...), args...)}, &buildViewLimitWriter{w: f, left: limit})
	if err != nil || len(diagnostic) != 0 {
		f.Close()
		if err != nil {
			return nil, status, err
		}
		if bytes.Contains(diagnostic, []byte("sparse index")) {
			return nil, status, errors.New("native checkout metadata reported diagnostics: a sparse index is not supported")
		}
		return nil, status, errors.New("native checkout metadata reported diagnostics")
	}
	return f, status, nil
}
func (g *buildViewGraph) query(ctx context.Context, root string, args ...string) (string, error) {
	f, status, err := g.read(ctx, root, g.env, buildViewQueryLimit, args...)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if status != 0 {
		return "", errors.New("native checkout setup failed")
	}
	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.NewSectionReader(f, 0, stat.Size()))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' || bytes.IndexByte(raw, 0) >= 0 {
		return "", errors.New("invalid native checkout setup response")
	}
	return string(raw[:len(raw)-1]), nil
}
func (g *buildViewGraph) copyBinding(b *buildViewBinding, path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err == nil {
		err = b.copyTo(f)
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Chtimes(path, time.Unix(0, 0), b.info.ModTime()); err != nil {
		return err
	}
	actual, err := os.Stat(path)
	if err != nil || actual.ModTime() != b.info.ModTime() {
		return errors.New("private checkout timestamp cannot be represented")
	}
	return nil
}

// copyUnbound copies a file through a descriptor-relative binding that is closed at
// once: nothing about the source name or inode is validated later. It is for the index,
// which a project step may legitimately rewrite; its meaning is compared separately.
func (g *buildViewGraph) copyUnbound(ctx context.Context, source, destination string, optional bool) error {
	b, err := captureBuildViewPath(ctx, source, nil)
	if err != nil {
		return errors.New("cannot bind checkout metadata")
	}
	defer b.Close()
	if b.file == nil {
		if optional {
			return nil
		}
		return errors.New("required checkout metadata is absent")
	}
	return g.copyBinding(b, destination)
}
func (g *buildViewGraph) captureRoot(ctx context.Context, path, expected string) error {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New("cannot resolve checkout root")
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return err
	}
	if g.seen[canonical] {
		return nil
	}
	rootBinding, err := g.bind(ctx, canonical, false)
	if err != nil {
		return err
	}
	top, err := g.query(ctx, canonical, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(top)
	if err != nil {
		return err
	}
	actualInfo, err := os.Stat(actual)
	if err != nil || !os.SameFile(rootBinding.info, actualInfo) {
		return errors.New("effective Git worktree differs from expected checkout")
	}
	canonical = actual
	if g.seen[canonical] {
		return nil
	}
	if len(g.roots) >= buildViewMaxRoots {
		return fmt.Errorf("%w: too many nested checkouts", errReleaseLimit)
	}
	g.seen[canonical] = true
	gitdir, err := g.query(ctx, canonical, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	common, err := g.query(ctx, canonical, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	objects, err := g.query(ctx, canonical, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return err
	}
	head, err := g.query(ctx, canonical, "rev-parse", "--verify", "HEAD")
	if err != nil || !IsFullCommitID(head) {
		return errors.New("cannot resolve checkout HEAD")
	}
	if expected != "" && head != expected {
		return errors.New("HEAD differs from requested commit")
	}
	format, err := g.query(ctx, canonical, "rev-parse", "--show-object-format")
	if err != nil {
		return err
	}
	hashBytes := 20
	if format == "sha256" {
		hashBytes = 32
	} else if format != "sha1" {
		return errors.New("invalid native object format")
	}
	for _, p := range []string{gitdir, common, objects} {
		if !filepath.IsAbs(p) {
			return errors.New("native checkout metadata path is not absolute")
		}
	}
	gitdir, err = filepath.EvalSymlinks(gitdir)
	if err != nil {
		return err
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil {
		return err
	}
	gitBinding, err := g.bind(ctx, gitdir, false)
	if err != nil {
		return err
	}
	commonBinding, err := g.bind(ctx, common, false)
	if err != nil {
		return err
	}
	anchor, err := g.bind(ctx, filepath.Join(canonical, ".git"), true)
	if err != nil {
		return err
	}
	if anchor.file != nil && anchor.info.Mode().IsRegular() {
		if err = g.copyBinding(anchor, filepath.Join(g.options.Private, fmt.Sprintf("anchor-%d", len(g.roots)))); err != nil {
			return err
		}
	}
	pointer, err := g.bind(ctx, filepath.Join(gitdir, "commondir"), true)
	if err != nil {
		return err
	}
	if pointer.file != nil && pointer.info.Mode().IsRegular() {
		if err = g.copyBinding(pointer, filepath.Join(g.options.Private, fmt.Sprintf("common-pointer-%d", len(g.roots)))); err != nil {
			return err
		}
	}
	base := filepath.Join(g.options.Private, fmt.Sprintf("root-%d", len(g.roots)))
	if err = os.Mkdir(base, 0700); err != nil {
		return err
	}
	if err = os.Chmod(base, 0700); err != nil {
		return err
	}
	privateGit := base
	privateCommon := base
	if gitdir != common || pointer.info != nil {
		privateCommon = filepath.Join(base, "common")
		if err = os.Mkdir(privateCommon, 0700); err != nil {
			return err
		}
		if err = os.Chmod(privateCommon, 0700); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(privateGit, "commondir"), []byte(privateCommon+"\n"), 0600); err != nil {
			return err
		}
	}
	root := buildViewRoot{Identity: buildViewFileIdentity(rootBinding.info), GitdirIdentity: buildViewFileIdentity(gitBinding.info), CommonIdentity: buildViewFileIdentity(commonBinding.info), Anchor: snapshotBuildViewMetadata(filepath.Join(canonical, ".git"), anchor), CommonPointer: snapshotBuildViewMetadata(filepath.Join(gitdir, "commondir"), pointer), Worktree: buildViewPath(canonical), Gitdir: buildViewPath(gitdir), Common: buildViewPath(common), Objects: buildViewPath(objects), PrivateGitdir: buildViewPath(privateGit), PrivateCommon: buildViewPath(privateCommon), Head: head, hashBytes: hashBytes}
	names, values, err := g.observeConfig(ctx, canonical, g.env)
	if err != nil {
		return err
	}
	root.Names, root.Values = buildViewPath(names.Name()), buildViewPath(values.Name())
	defer names.Close()
	defer values.Close()
	if err = g.bindConfigOrigins(ctx, canonical, names); err != nil {
		return err
	}
	local, err := os.OpenFile(filepath.Join(privateCommon, "config"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer local.Close()
	if err = local.Chmod(0600); err != nil {
		return err
	}
	worktree, err := os.OpenFile(filepath.Join(privateGit, "config.worktree"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer worktree.Close()
	if err = worktree.Chmod(0600); err != nil {
		return err
	}
	if err = renderBuildViewConfig(names, values, local, worktree); err != nil {
		return err
	}
	// GIT_WORK_TREE is authoritative, and the stored setting must not rebase a
	// relative original core.worktree against the new private metadata directory.
	for _, out := range []*os.File{local, worktree} {
		if _, err = io.WriteString(out, "[core]\nworktree = \""); err != nil {
			return err
		}
		if err = escapeBuildViewConfig(out, strings.NewReader(canonical), true); err != nil {
			return err
		}
		if _, err = io.WriteString(out, "\"\n"); err != nil {
			return err
		}
	}
	for _, name := range []string{"refs", "reftable", "objects", "info", "modules", "packed-refs", "shallow"} {
		if err = os.Symlink(filepath.Join(common, name), filepath.Join(privateCommon, name)); err != nil {
			return err
		}
	}
	if privateGit != privateCommon {
		for _, name := range []string{"refs", "reftable"} {
			if err = os.Symlink(filepath.Join(gitdir, name), filepath.Join(privateGit, name)); err != nil {
				return err
			}
		}
		if err = os.Symlink(filepath.Join(common, "HEAD"), filepath.Join(privateCommon, "HEAD")); err != nil {
			return err
		}
	}
	headBinding, err := g.bind(ctx, filepath.Join(gitdir, "HEAD"), true)
	if err != nil {
		return err
	}
	if headBinding.rawSymlink {
		target := headBinding.linkTarget
		if !filepath.IsAbs(target) && !strings.HasPrefix(target, "refs/") {
			target = filepath.Join(gitdir, target)
		}
		if err = os.Symlink(target, filepath.Join(privateGit, "HEAD")); err != nil {
			return err
		}
	} else if err = g.copyBinding(headBinding, filepath.Join(privateGit, "HEAD")); err != nil {
		return err
	}
	if err = g.copyUnbound(ctx, filepath.Join(gitdir, "index"), filepath.Join(privateGit, "index"), true); err != nil {
		return err
	}
	if index, openErr := os.Open(filepath.Join(privateGit, "index")); openErr == nil {
		dependency, e := buildViewIndexDependency(index, hashBytes)
		index.Close()
		if e != nil {
			return e
		}
		if dependency != "" {
			if e = g.copyUnbound(ctx, filepath.Join(gitdir, dependency), filepath.Join(privateGit, dependency), false); e != nil {
				return e
			}
		}
	} else if !errors.Is(openErr, os.ErrNotExist) {
		return openErr
	}
	// Inspect actual private effective config, not only the intended render.
	pn, pv, err := g.observeConfig(ctx, canonical, g.privateEnv(root))
	if err != nil {
		return err
	}
	err = renderBuildViewConfig(pn, pv, io.Discard, io.Discard)
	pn.Close()
	pv.Close()
	if err != nil {
		return err
	}
	digest, err := g.indexListing(ctx, root)
	if err != nil {
		return err
	}
	g.roots = append(g.roots, root)
	g.indexSeal = append(g.indexSeal, digest)
	listing, status, err := g.read(ctx, canonical, g.privateEnv(root), buildViewListingLimit, "ls-files", "--stage", "-z", "--full-name", "--")
	if err != nil {
		return err
	}
	defer listing.Close()
	if status != 0 {
		return errors.New("private native index validation failed")
	}
	return g.captureGitlinks(ctx, root, listing)
}
func (g *buildViewGraph) privateEnv(root buildViewRoot) []string {
	return privateBuildViewEnvironment(g.env, root, g.route, g.manifest)
}

func (g *buildViewGraph) observeConfig(ctx context.Context, path string, env []string) (*os.File, *os.File, error) {
	args := []string{"config", "--includes", "--null", "--show-scope", "--show-origin", "--list"}
	names, status, err := g.read(ctx, path, env, buildViewListingLimit, append(args, "--name-only")...)
	if err != nil {
		return nil, nil, err
	}
	if status != 0 {
		names.Close()
		return nil, nil, errors.New("native configuration names failed")
	}
	values, status, err := g.read(ctx, path, env, buildViewListingLimit, args...)
	if err != nil {
		names.Close()
		return nil, nil, err
	}
	if status != 0 {
		names.Close()
		values.Close()
		return nil, nil, errors.New("native configuration values failed")
	}
	return names, values, nil
}
func (g *buildViewGraph) bindConfigOrigins(ctx context.Context, root string, names *os.File) error {
	frames := newBuildViewFrames(names)
	seen := map[string]bool{}
	for {
		scope, err := frames.next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("invalid native configuration origin framing")
		}
		origin, err := frames.next()
		if err != nil {
			return err
		}
		if _, err = frames.next(); err != nil {
			return err
		}
		if buildViewFieldIs(names, scope, "command") {
			continue
		}
		raw, err := io.ReadAll(origin.reader(names))
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(raw, []byte("file:")) {
			return errors.New("invalid native configuration file origin")
		}
		path := string(raw[len("file:"):])
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		b, err := g.bind(ctx, path, false)
		if err != nil {
			return err
		}
		if err = g.copyBinding(b, filepath.Join(g.options.Private, fmt.Sprintf("origin-%d", len(g.bindings)))); err != nil {
			return err
		}
	}
}
func (g *buildViewGraph) captureGitlinks(ctx context.Context, root buildViewRoot, listing *os.File) error {
	r := bufio.NewReader(io.NewSectionReader(listing, 0, 1<<63-1))
	for {
		header, err := r.ReadString('\t')
		if err == io.EOF && header == "" {
			return nil
		}
		if err != nil {
			return errors.New("invalid private native index framing")
		}
		fields := strings.Fields(strings.TrimSuffix(header, "\t"))
		if len(fields) != 3 || !IsFullCommitID(fields[1]) || len(fields[2]) != 1 || fields[2][0] < '0' || fields[2][0] > '3' {
			return errors.New("invalid private native index header")
		}
		if fields[0] != "160000" {
			for {
				_, e := r.ReadSlice(0)
				if e == nil {
					break
				}
				if e != bufio.ErrBufferFull {
					return errors.New("invalid private native index path")
				}
			}
			continue
		}
		name, e := r.ReadString(0)
		if e != nil {
			return errors.New("invalid private native gitlink path")
		}
		name = strings.TrimSuffix(name, "\x00")
		if !filepath.IsLocal(name) {
			return errors.New("unsafe private native gitlink path")
		}
		child := filepath.Join(string(root.Worktree), name)
		b, e := g.bind(ctx, filepath.Join(child, ".git"), true)
		if e != nil {
			return e
		}
		if b.info == nil {
			continue
		} // Only the observed absent .git exception is uninitialized.
		if e = g.captureRoot(ctx, child, ""); e != nil {
			return e
		}
	}
}
func (g *buildViewGraph) validate(ctx context.Context) error {
	if err := g.verifySeal(); err != nil {
		return err
	}
	for _, b := range g.bindings {
		if err := b.validate(); err != nil {
			return err
		}
	}
	for _, root := range g.roots {
		names, values, err := g.observeConfig(ctx, string(root.Worktree), g.env)
		if err != nil {
			return err
		}
		same := func(current *os.File, expected buildViewPath) bool {
			old, e := os.Open(string(expected))
			if e != nil {
				return false
			}
			defer old.Close()
			a, e := current.Stat()
			if e != nil {
				return false
			}
			b, e := old.Stat()
			if e != nil {
				return false
			}
			return equalBuildViewFields(current, buildViewField{length: a.Size()}, old, buildViewField{length: b.Size()})
		}
		matches := same(names, root.Names) && same(values, root.Values)
		names.Close()
		values.Close()
		if !matches {
			return errors.New("captured native configuration changed")
		}
		head, err := g.query(ctx, string(root.Worktree), "rev-parse", "--verify", "HEAD")
		if err != nil || head != root.Head {
			return errors.New("captured checkout HEAD changed")
		}
	}
	return nil
}
func (g *buildViewGraph) check(ctx context.Context) error {
	if err := g.validate(ctx); err != nil {
		return err
	}
	if err := g.checkIndexes(ctx, true); err != nil {
		return err
	}
	return g.validate(ctx)
}
