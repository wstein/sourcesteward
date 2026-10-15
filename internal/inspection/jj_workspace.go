// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// JJWorkspaceState is a sampled native precondition, never authorization.
type JJWorkspaceState struct {
	Source                                                                         JJRepository
	BaseSelector, BaseCommit, BaseChange, RegistrationDigest, SafetyDigest, GitDir string
}

type JJWorkspaceCreation struct {
	Workspace            *JJRepository
	Phase                string
	SourceUnchanged      bool
	Effects, Limitations []string
}

// ObserveJJWorkspace evaluates one explicit native base without snapshots or config execution.
func ObserveJJWorkspace(ctx context.Context, path, base string) (JJWorkspaceState, error) {
	var state JJWorkspaceState
	if base == "" || len(base) > 4096 || strings.ContainsRune(base, 0) {
		return state, errors.New("invalid explicit jj base")
	}
	identity := Inspect(ctx, path)
	if identity.Status != "ok" || identity.Backend != "jj" || identity.JJRepository == nil {
		return state, errors.New("supported unambiguous native jj workspace required")
	}
	state.Source = *identity.JJRepository
	state.BaseSelector = base
	err := withJJSession(ctx, path, identity, func(s jjSession, root, metadata, repo string) error {
		observed, e := observeJJ(ctx, root, metadata, repo, func(op string, a ...string) (string, error) { return s.read(path, op, a...) })
		if e != nil {
			return e
		}
		if observed != state.Source {
			return errJJChanged
		}
		out, e := s.read(path, observed.OperationID, "log", "--revisions", base, "--no-graph", "--template", jjInventoryIDs)
		if e != nil {
			return e
		}
		ids, e := scalarRecord(out, 2)
		if e != nil || !identifier(ids[0], 40, "0123456789abcdef") || !identifier(ids[1], 32, "klmnopqrstuvwxyz") {
			return errors.New("jj base must resolve to exactly one full native commit")
		}
		state.BaseCommit = ids[0]
		state.BaseChange = ids[1]
		out, e = s.read(path, observed.OperationID, "workspace", "list", "--template", jjInventoryWorkspaces)
		if e != nil {
			return e
		}
		state.RegistrationDigest = jjWorkspaceDigest(out)
		state.GitDir, e = jjWorkspaceGitDir(repo)
		if e != nil {
			return e
		}
		state.SafetyDigest, e = jjWorkspaceSourceDigest(metadata, state.GitDir)
		if e != nil {
			return e
		}
		final, e := observeJJ(ctx, root, metadata, repo, func(op string, a ...string) (string, error) { return s.read(path, op, a...) })
		if e != nil {
			return e
		}
		if final != observed {
			return errJJChanged
		}
		return nil
	})
	return state, err
}

func validJJTaskName(name string) bool {
	if name == "" || len(name) > 128 || name[0] == '-' {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func jjWorkspaceDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func jjWorkspaceGitDir(repo string) (string, error) {
	typ, e := metadataFile(filepath.Join(repo, "store", "type"))
	if e != nil || string(typ) != "git" {
		return "", errors.New("jj workspace creation requires supported native Git-backed storage")
	}
	target, e := metadataFile(filepath.Join(repo, "store", "git_target"))
	if e != nil || len(target) == 0 || strings.ContainsAny(string(target), "\x00\r\n") {
		return "", errors.New("invalid jj Git store pointer")
	}
	dir := string(target)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, "store", dir)
	}
	dir = filepath.Clean(dir)
	for _, p := range []string{dir, filepath.Join(dir, "objects"), filepath.Join(dir, "refs"), filepath.Join(dir, "refs", "jj")} {
		if e = metadataDir(p); e != nil {
			return "", fmt.Errorf("unsafe or unsupported jj Git store: %w", e)
		}
	}
	// An embedded backing store can have independent manager markers even if source .git is absent.
	for _, p := range []string{"gitbutler", "refs/gitbutler", "refs/heads/gitbutler/workspace"} {
		if _, e = os.Lstat(filepath.Join(dir, p)); !os.IsNotExist(e) {
			return "", errors.New("ambiguous GitButler backing-store ownership")
		}
	}
	b, e := metadataFile(filepath.Join(dir, "packed-refs"))
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && (strings.HasPrefix(f[1], "refs/gitbutler/") || f[1] == "refs/heads/gitbutler/workspace") {
			return "", errors.New("ambiguous GitButler backing-store ownership")
		}
	}
	return dir, nil
}

// A bounded digest protects source checkout metadata/configuration, excluding declared shared stores.
func jjWorkspaceSourceDigest(metadata, git string) (string, error) {
	paths := []string{filepath.Join(metadata, "repo"), filepath.Join(metadata, "workspace-config-id"), filepath.Join(metadata, "workspace-config.toml"), filepath.Join(git, "HEAD"), filepath.Join(git, "index"), filepath.Join(git, "config"), filepath.Join(git, "config.worktree")}
	d, e := openMetadataDirectory(filepath.Join(metadata, "working_copy"))
	if e != nil {
		return "", e
	}
	entries, e := d.ReadDir(17)
	d.Close()
	if e != nil && len(entries) == 0 {
		return "", e
	}
	if len(entries) > 16 {
		return "", errors.New("jj working-copy metadata file-count bound exceeded")
	}
	for _, entry := range entries {
		paths = append(paths, filepath.Join(metadata, "working_copy", entry.Name()))
	}
	h := sha256.New()
	for _, p := range paths {
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			fmt.Fprintf(h, "%q absent\n", p)
			continue
		}
		if e != nil {
			return "", e
		}
		if st.IsDir() {
			continue
		}
		b, e := metadataFile(p)
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%q %v %x\n", p, st.Mode(), b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CreateJJWorkspace grants only declared native store/destination writes. It never resumes a partial creation.
func CreateJJWorkspace(ctx context.Context, path string, expected JJWorkspaceState, name, destination string) (JJWorkspaceCreation, error) {
	result := JJWorkspaceCreation{Phase: "not-started", Effects: []string{}, Limitations: []string{"jj registration and checkout are separate native phases; interrupted state is retained for manual recovery.", "Operation checks are sampled, not a compare-and-swap transaction; concurrent/ABA changes can be missed.", "Destination materialization uses native update-stale without operation pinning; source files are never snapshotted.", "A generated working-copy commit uses SourceSteward <stw@localhost>; no user identity is inferred."}}
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	if !validJJTaskName(name) || !filepath.IsAbs(destination) {
		return result, errors.New("invalid jj task name or destination")
	}
	if e := metadataDir(destination); e != nil {
		return result, e
	}
	if within(expected.Source.WorkspaceRoot, destination) || within(destination, expected.Source.WorkspaceRoot) || within(expected.Source.RepositoryDir, destination) || within(destination, expected.Source.RepositoryDir) {
		return result, errors.New("jj destination overlaps source or shared metadata")
	}
	dir, e := os.Open(destination)
	if e != nil {
		return result, e
	}
	entries, e := dir.ReadDir(1)
	dir.Close()
	if len(entries) != 0 {
		return result, errors.New("jj destination is occupied")
	}
	if e != nil && !errors.Is(e, io.EOF) {
		return result, e
	}
	fresh, e := ObserveJJWorkspace(ctx, path, expected.BaseSelector)
	if e != nil {
		return result, e
	}
	if !reflect.DeepEqual(fresh, expected) {
		return result, errJJChanged
	}
	identity := Inspect(ctx, path)
	if identity.Status != "ok" || identity.JJRepository == nil {
		return result, errors.New("cannot revalidate source jj identity")
	}
	e = withJJSession(ctx, path, identity, func(s jjSession, root, metadata, repo string) error {
		// No command can invoke configured helpers; colocation is explicitly off even in jj0.46.
		roots := []string{repo, filepath.Join(expected.GitDir, "objects"), filepath.Join(expected.GitDir, "refs", "jj"), destination}
		for _, p := range roots {
			if e := metadataDir(p); e != nil {
				return e
			}
		}
		observed, e := observeJJ(ctx, root, metadata, repo, func(op string, a ...string) (string, error) { return s.read(path, op, a...) })
		if e != nil {
			return e
		}
		if observed != expected.Source {
			return errJJChanged
		}
		result.Phase = "unknown"
		result.Effects = []string{"Owned destination files and jj working-copy metadata may be created.", "Shared jj repository/index/operation storage, backing Git objects and refs/jj keep references may be written."}
		_, code, e := s.workspaceExecute(path, roots, []string{"--ignore-working-copy", "--at-operation=" + expected.Source.OperationID, "workspace", "add", "--name", name, "--revision", expected.BaseCommit, "--sparse-patterns", "full", destination})
		if e != nil {
			return e
		}
		if code != 0 {
			return errors.New("native jj workspace registration failed; inspect retained recovery evidence")
		}
		result.Phase = "registered"
		created, e := jjWorkspaceVerifySession(ctx, s, destination, name, repo)
		if e != nil {
			return e
		}
		result.Workspace = &created
		parents, e := s.read(destination, created.OperationID, "log", "--revisions", "@-", "--no-graph", "--template", `json(self.commit_id()) ++ "\n"`)
		if e != nil {
			return e
		}
		p, e := scalarRecord(parents, 1)
		if e != nil || p[0] != expected.BaseCommit {
			return errors.New("created jj workspace does not have the expected explicit parent")
		}
		// update-stale rejects --at-operation. Sample immediately before and after; never claim a CAS.
		again, e := jjWorkspaceVerifySession(ctx, s, destination, name, repo)
		if e != nil {
			return e
		}
		if again != created {
			return errJJChanged
		}
		_, code, e = s.workspaceExecute(destination, roots, []string{"workspace", "update-stale"})
		if e != nil {
			return e
		}
		if code != 0 {
			return errors.New("native jj workspace materialization failed; registered workspace retained")
		}
		final, e := jjWorkspaceVerifySession(ctx, s, destination, name, repo)
		if e != nil {
			return e
		}
		result.Workspace = &final
		if final.CommitID != created.CommitID || final.ChangeID != created.ChangeID || final.OperationID != created.OperationID {
			return errors.New("jj workspace changed during materialization")
		}
		digest, e := jjWorkspaceSourceDigest(metadata, expected.GitDir)
		if e != nil {
			return e
		}
		if digest != expected.SafetyDigest {
			return errors.New("source checkout metadata changed during creation")
		}
		source, e := observeJJ(ctx, root, metadata, repo, func(op string, a ...string) (string, error) { return s.read(path, op, a...) })
		if e != nil {
			return e
		}
		if source.CommitID != expected.Source.CommitID || source.ChangeID != expected.Source.ChangeID || source.WorkspaceName != expected.Source.WorkspaceName {
			return errors.New("source native working-copy identity changed")
		}
		result.SourceUnchanged = true
		result.Phase = "materialized"
		return nil
	})
	return result, e
}

func jjWorkspaceVerifySession(ctx context.Context, s jjSession, destination, name, repo string) (JJRepository, error) {
	metadata := filepath.Join(destination, ".jj")
	resolved, e := resolveJJRepository(metadata)
	if e != nil || resolved != repo {
		return JJRepository{}, errors.New("foreign jj task repository")
	}
	j, e := observeJJ(ctx, destination, metadata, repo, func(op string, a ...string) (string, error) { return s.read(destination, op, a...) })
	if e != nil {
		return j, e
	}
	if j.WorkspaceName != name {
		return JJRepository{}, errors.New("foreign jj workspace name")
	}
	return j, nil
}

// VerifyJJWorkspace accepts legitimate progressed task identities while checking stable native registration.
func VerifyJJWorkspace(ctx context.Context, source, name, destination, repo string) (JJRepository, error) {
	var j JJRepository
	identity := Inspect(ctx, source)
	if identity.Status != "ok" || identity.JJRepository == nil || identity.JJRepository.RepositoryDir != repo {
		return j, errors.New("source jj repository identity changed")
	}
	e := withJJSession(ctx, source, identity, func(s jjSession, _, _, _ string) error {
		var e error
		j, e = jjWorkspaceVerifySession(ctx, s, destination, name, repo)
		return e
	})
	return j, e
}

func (s jjSession) workspaceExecute(dir string, roots, args []string) (string, int, error) {
	prefix := []string{"--no-pager", "--color=never", "--config", "git.colocate=false", "--config", `user.name="SourceSteward"`, "--config", `user.email="stw@localhost"`}
	var argv []string
	if runtime.GOOS == "darwin" {
		profile := `(version 1)(allow default)(deny file-write*)(deny network*)(deny process-exec)(allow process-exec (literal ` + strconv.Quote(s.bin) + `))(allow file-write*`
		for _, root := range roots {
			profile += ` (subpath ` + strconv.Quote(root) + `)`
		}
		profile += `)`
		argv = append([]string{"-p", profile, s.bin}, append(prefix, args...)...)
	} else {
		argv = []string{"--ro-bind", "/", "/", "--unshare-net", "--die-with-parent", "--new-session", "--cap-drop", "ALL"}
		for _, root := range roots {
			argv = append(argv, "--bind", root, root)
		}
		argv = append(argv, "--", s.bin)
		argv = append(argv, prefix...)
		argv = append(argv, args...)
	}
	cmd := exec.CommandContext(s.ctx, s.sandbox, argv...)
	cmd.Dir = dir
	cmd.Env = s.env
	cmd.Stdin = strings.NewReader("")
	cmd.WaitDelay = time.Second
	var out, errout capture
	cmd.Stdout = &out
	cmd.Stderr = &errout
	e := cmd.Run()
	if s.ctx.Err() != nil {
		return "", -1, s.ctx.Err()
	}
	if out.overflow || errout.overflow {
		return "", -1, errJJOutput
	}
	if e == nil {
		return out.String(), 0, nil
	}
	var exit *exec.ExitError
	if errors.As(e, &exit) {
		return out.String(), exit.ExitCode(), nil
	}
	return "", -1, e
}
