// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const buildViewRole = "STW_BUILD_VIEW_ROLE"
const buildViewManifestVariable = "STW_BUILD_VIEW_MANIFEST"

type buildViewManifest struct {
	Version                      int
	Private, RealGit, Executable buildViewPath
	Env                          []buildViewPath
	Roots                        []buildViewRoot
}

func (g *buildViewGraph) writeManifest() error {
	g.route = filepath.Join(g.options.Private, "route")
	g.manifest = filepath.Join(g.options.Private, "manifest.json")
	if err := os.Mkdir(g.route, 0700); err != nil {
		return err
	}
	if err := os.Chmod(g.route, 0700); err != nil {
		return err
	}
	if err := os.Symlink(g.options.Executable, filepath.Join(g.route, "git")); err != nil {
		return err
	}
	env := make([]buildViewPath, len(g.env))
	for i, e := range g.env {
		env[i] = buildViewPath(e)
	}
	f, err := os.OpenFile(g.manifest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err == nil {
		err = json.NewEncoder(f).Encode(buildViewManifest{Version: 1, Private: buildViewPath(g.options.Private), RealGit: buildViewPath(g.options.RealGit), Executable: buildViewPath(g.options.Executable), Env: env, Roots: g.roots})
	}
	return errors.Join(err, f.Close())
}
func privateBuildViewEnvironment(base []string, root buildViewRoot, route, manifest string) []string {
	var env []string
	path := ""
	for _, entry := range base {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
			continue
		}
		env = append(env, entry)
	}
	if route != "" {
		path = route + ":" + path
		env = append(env, "GIT_EXEC_PATH="+route, buildViewRole+"=route", buildViewManifestVariable+"="+manifest)
	}
	return append(env, "PATH="+path, "GIT_DIR="+string(root.PrivateGitdir), "GIT_COMMON_DIR="+string(root.PrivateCommon), "GIT_INDEX_FILE="+filepath.Join(string(root.PrivateGitdir), "index"), "GIT_WORK_TREE="+string(root.Worktree), "GIT_OBJECT_DIRECTORY="+string(root.Objects))
}
func readBuildViewManifest(path string) (buildViewManifest, error) {
	var m buildViewManifest
	if !filepath.IsAbs(path) {
		return m, errors.New("invalid private route manifest path")
	}
	private := filepath.Dir(path)
	directory, err := os.OpenRoot(private)
	if err != nil {
		return m, err
	}
	defer directory.Close()
	di, err := directory.Stat(".")
	if err != nil || di.Mode().Perm() != 0700 || buildViewFileIdentity(di).UID != uint32(os.Getuid()) {
		return m, errors.New("invalid private route directory ownership")
	}
	file, err := openBuildViewFile(directory, filepath.Base(path))
	if err != nil {
		return m, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || buildViewFileIdentity(info).UID != uint32(os.Getuid()) {
		return m, errors.New("invalid private route manifest ownership")
	}
	if err = json.NewDecoder(file).Decode(&m); err != nil {
		return m, errors.New("invalid private route manifest framing")
	}
	if m.Version != 1 || string(m.Private) != private || !filepath.IsAbs(string(m.RealGit)) || !filepath.IsAbs(string(m.Executable)) {
		return m, errors.New("invalid private route manifest binding")
	}
	actual, err := os.Executable()
	if err != nil {
		return m, err
	}
	a, err := os.Stat(actual)
	if err != nil {
		return m, err
	}
	expected, err := os.Stat(string(m.Executable))
	if err != nil || !os.SameFile(a, expected) {
		return m, errors.New("private route executable changed")
	}
	return m, nil
}
func runBuildViewRoute(ctx context.Context, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !(len(args) == 2 || len(args) == 3) || args[0] != "status" || args[1] != "--porcelain=2" || (len(args) == 3 && args[2] != "-uno") {
		return errors.New("unsupported private native child argv")
	}
	m, err := readBuildViewManifest(os.Getenv(buildViewManifestVariable))
	if err != nil {
		return err
	}
	cwd, err := os.Open(".")
	if err != nil {
		return err
	}
	defer cwd.Close()
	current, err := cwd.Stat()
	if err != nil {
		return err
	}
	name, err := os.Getwd()
	if err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(name)
	if err != nil {
		return err
	}
	for _, root := range m.Roots {
		if buildViewFileIdentity(current) != root.Identity || canonical != string(root.Worktree) {
			continue
		}
		for _, item := range []struct {
			path     buildViewPath
			identity buildViewIdentity
		}{{root.Gitdir, root.GitdirIdentity}, {root.Common, root.CommonIdentity}} {
			b, e := captureBuildViewPath(ctx, string(item.path), nil)
			if e != nil {
				return errors.New("private native child metadata unavailable")
			}
			resolved, e := filepath.EvalSymlinks(string(item.path))
			valid := e == nil && resolved == string(item.path) && b.info != nil && buildViewFileIdentity(b.info) == item.identity
			if valid {
				e = b.validate()
			}
			b.Close()
			if !valid || e != nil {
				return errors.New("private native child metadata binding changed")
			}
		}
		if err = root.Anchor.verify(ctx); err != nil {
			return err
		}
		if err = root.CommonPointer.verify(ctx); err != nil {
			return err
		}
		inherited := make([]string, len(m.Env))
		for i, e := range m.Env {
			inherited[i] = string(e)
		}
		base := buildViewEnvironment(inherited, string(m.RealGit), string(m.Private))
		env := privateBuildViewEnvironment(base, root, filepath.Join(string(m.Private), "route"), os.Getenv(buildViewManifestVariable))
		if err := ctx.Err(); err != nil {
			return err
		}
		return execBuildViewRoute(buildViewCommand{Executable: string(m.RealGit), Env: env, Args: append(append([]string{}, buildViewGitPrefix...), args...)})
	}
	return errors.New("unknown private native child root")
}
