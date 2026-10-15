// SPDX-License-Identifier: EUPL-1.2
package workspace

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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/inspection"
)

type gitAdapter struct{}

func gitExecutable() (string, error) {
	p, err := exec.LookPath("git")
	if err == nil {
		p, err = filepath.Abs(p)
	}
	if err == nil {
		p, err = filepath.EvalSymlinks(p)
	}
	if err != nil {
		return "", failure("git-unavailable", "cannot resolve the supported Git executable")
	}
	if runtime.GOOS == "darwin" && p == "/usr/bin/git" {
		return "", failure("git-sandbox-unavailable", "select a native Git executable on PATH; the Apple /usr/bin/git launcher cannot run inside the required executable sandbox")
	}
	return p, nil
}

func gitNativeIdentity(ctx context.Context, path string) (NativeIdentity, error) {
	r := inspection.Inspect(ctx, path)
	if r.Status != "ok" || r.Backend != "git" || r.Repository == nil || r.Repository.Bare || r.Repository.Worktree == nil || r.Repository.Head.OID == nil {
		return NativeIdentity{}, failure("git-ownership-refused", "workspace creation requires an unambiguous ordinary Git checkout with a committed HEAD")
	}
	repo := r.Repository
	id := NativeIdentity{Backend: "git", RepositoryDir: repo.CommonDir, WorkspaceRoot: *repo.Worktree, GitDir: repo.GitDir, ObjectFormat: repo.ObjectFormat, CommitID: *repo.Head.OID}
	if repo.Head.Ref != nil {
		id.HeadRef = *repo.Head.Ref
	}
	for _, p := range []string{id.RepositoryDir, id.WorkspaceRoot, id.GitDir} {
		canonical, e := filepath.EvalSymlinks(p)
		if e != nil || canonical != p {
			return NativeIdentity{}, failure("git-metadata-refused", "Git identity paths must be canonical and accessible")
		}
	}
	return id, nil
}

func gitRead(ctx context.Context, bin, dir string, args ...string) (string, error) {
	out, code, err := runSandboxed(ctx, bin, append([]string{"--no-replace-objects"}, args...), dir, nil, nil)
	if err != nil {
		return "", gitExecutionError(err)
	}
	if code != 0 || !utf8.ValidString(out) {
		return "", failure("git-query-refused", "bounded native Git safety evidence is unavailable")
	}
	return out, nil
}

func gitExecutionError(err error) error {
	if errors.Is(err, errNativeSandboxUnavailable) {
		return failure("git-sandbox-unavailable", "native Git workspace operations require an available filesystem and network sandbox; select a native Git executable on PATH")
	}
	if errors.Is(err, errNativeOutputLimit) {
		return failure("git-output-limit", "native Git workspace evidence exceeded the 64 KiB command output bound")
	}
	return err
}

func gitDigest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func fullGitOID(s, format string) bool {
	n := 40
	if format == "sha256" {
		n = 64
	}
	if len(s) != n {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (gitAdapter) Observe(ctx context.Context, path, base string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id, err := gitNativeIdentity(ctx, path)
	if err != nil {
		return Snapshot{}, err
	}
	bin, err := gitExecutable()
	if err != nil {
		return Snapshot{}, err
	}
	if !fullGitOID(base, id.ObjectFormat) {
		if base != "HEAD" && !strings.HasPrefix(base, "refs/") || len(base) > 4096 || strings.ContainsAny(base, "\x00\r\n") {
			return Snapshot{}, failure("git-base-refused", "base must be a full Git commit ID or a literal fully qualified reference")
		}
		if base != "HEAD" {
			if _, err = gitRead(ctx, bin, id.WorkspaceRoot, "check-ref-format", base); err != nil {
				return Snapshot{}, failure("git-base-refused", "base is not a valid literal fully qualified reference")
			}
		}
	}
	baseRaw, err := gitRead(ctx, bin, id.WorkspaceRoot, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return Snapshot{}, err
	}
	commit := strings.TrimSuffix(baseRaw, "\n")
	if !fullGitOID(commit, id.ObjectFormat) {
		return Snapshot{}, failure("git-base-refused", "cannot resolve a complete base commit identity")
	}
	safety, err := gitSafety(ctx, bin, id, commit)
	if err != nil {
		return Snapshot{}, err
	}
	refs, err := gitRead(ctx, bin, id.WorkspaceRoot, "for-each-ref", "--format=%(refname)%00%(objectname)%00")
	if err != nil {
		return Snapshot{}, err
	}
	if strings.Count(refs, "\n") > observationLimit {
		return Snapshot{}, failure("git-observation-limit", "reference inventory exceeds the workspace observation limit")
	}
	registrations, err := gitRead(ctx, bin, id.WorkspaceRoot, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return Snapshot{}, err
	}
	if strings.Count(registrations, "worktree ") > observationLimit {
		return Snapshot{}, failure("git-observation-limit", "worktree inventory exceeds the workspace observation limit")
	}
	return Snapshot{Identity: id, BaseSelector: base, BaseCommit: commit, ReferenceDigest: gitDigest(refs), RegistrationDigest: gitDigest(registrations), SafetyDigest: safety}, nil
}

func gitSafety(ctx context.Context, bin string, id NativeIdentity, commit string) (string, error) {
	config, err := gitRead(ctx, bin, id.WorkspaceRoot, "config", "--null", "--list", "--includes")
	if err != nil {
		return "", err
	}
	for _, entry := range strings.Split(strings.TrimSuffix(config, "\x00"), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		unsafe := strings.HasPrefix(key, "filter.") || strings.HasPrefix(key, "hook.") || strings.HasPrefix(key, "includeif.") || key == "core.hookspath" || key == "core.attributesfile" || key == "core.alternaterefscommand" || key == "core.worktree" || key == "extensions.worktreeconfig" || key == "extensions.partialclone" || key == "core.sparsecheckout" || key == "core.sparsecheckoutcone"
		unsafe = unsafe || key == "core.fsmonitor" && !gitFalse(value) || key == "submodule.recurse" && !gitFalse(value) || strings.HasPrefix(key, "submodule.") || strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".promisor") && !gitFalse(value)
		if unsafe {
			return "", failure("git-unsafe-configuration", "configured hooks, checkout helpers or unsupported checkout configuration require an explicit supported policy before creation")
		}
	}
	// Relative/conditional hook paths are refused above; default hooks are shared.
	hooks, err := gitHooksFingerprint(filepath.Join(id.RepositoryDir, "hooks"))
	if err != nil {
		return "", err
	}
	for _, p := range []string{filepath.Join(id.GitDir, "config.worktree"), filepath.Join(id.GitDir, "info", "sparse-checkout"), filepath.Join(id.RepositoryDir, "objects", "info", "alternates")} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return "", failure("git-unsupported-metadata", "worktree-specific, sparse or alternate-object configuration is unsupported for task creation")
		}
	}
	tree, err := gitRead(ctx, bin, id.WorkspaceRoot, "ls-tree", "-r", "-l", "-z", "--full-tree", commit)
	if err != nil {
		return "", err
	}
	if err = validateGitTree(tree); err != nil {
		return "", err
	}
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() {
		return "", failure("git-unavailable", "Git executable identity is unavailable")
	}
	return gitDigest(config + "\x00" + hooks + "\x00" + tree + "\x00" + bin + "\x00" + fmt.Sprintf("%d:%d:%d", info.Mode(), info.Size(), info.ModTime().UnixNano())), nil
}
func gitFalse(s string) bool { return s == "false" || s == "no" || s == "off" || s == "0" }

func gitHooksFingerprint(dir string) (string, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", failure("git-hooks-refused", "cannot safely inspect the default Git hook directory")
	}
	f, err := os.Open(dir)
	if err != nil {
		return "", failure("git-hooks-refused", "cannot inspect Git hooks")
	}
	defer f.Close()
	entries, err := f.ReadDir(observationLimit + 1)
	if err != nil && err != io.EOF || len(entries) > observationLimit {
		return "", failure("git-hooks-refused", "Git hooks exceed the inspection bound")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var fingerprint strings.Builder
	for _, entry := range entries {
		st, e := entry.Info()
		if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 != 0 && !strings.HasSuffix(entry.Name(), ".sample") {
			return "", failure("git-hooks-refused", "executable hooks or unsupported hook directory entries prevent task creation; hooks are never silently skipped")
		}
		if st.Size() > 65536 {
			return "", failure("git-hooks-refused", "Git hook metadata exceeds the inspection bound")
		}
		fmt.Fprintf(&fingerprint, "%s:%d:%d:%d\x00", entry.Name(), st.Mode(), st.Size(), st.ModTime().UnixNano())
	}
	return fingerprint.String(), nil
}

func validateGitTree(raw string) error {
	entries := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	if raw == "" {
		entries = nil
	}
	if len(entries) > observationLimit {
		return failure("git-checkout-limit", "base checkout exceeds 1024 leaf entries")
	}
	var total int64
	for _, entry := range entries {
		meta, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return failure("git-tree-refused", "base contains unsupported tree objects, symlinks or gitlinks")
		}
		if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\x00") || !utf8.ValidString(path) {
			return failure("git-tree-refused", "base contains an unsupported checkout path")
		}
		for _, component := range strings.Split(path, "/") {
			if component == "." || component == ".." || strings.EqualFold(component, ".git") || component == "" {
				return failure("git-tree-refused", "base contains an unsafe checkout path")
			}
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || size < 0 || size > 16*1024*1024 {
			return failure("git-checkout-limit", "base blob exceeds 16 MiB or has unavailable size")
		}
		total += size
		if total > 64*1024*1024 {
			return failure("git-checkout-limit", "base checkout exceeds 64 MiB")
		}
	}
	return nil
}

func gitProtectedMetadata(common string) ([]string, error) {
	dir := filepath.Join(common, "worktrees")
	st, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err = os.Mkdir(dir, 0700); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, failure("git-metadata-refused", "Git worktree metadata is not an ordinary directory")
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(observationLimit + 1)
	if err != nil && err != io.EOF || len(entries) > observationLimit {
		return nil, failure("git-observation-limit", "existing worktree metadata exceeds the observation limit")
	}
	var roots []string
	for _, entry := range entries {
		st, e := entry.Info()
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, failure("git-metadata-refused", "existing worktree metadata contains unsupported entries")
		}
		roots = append(roots, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(roots)
	return roots, nil
}

func (gitAdapter) Create(ctx context.Context, p MutationPlan) (NativeWorkspace, error) {
	n := NativeWorkspace{Phase: "reserved", Effects: []string{}, Limitations: []string{"Native state is sampled; external races and ABA changes are not transactionally prevented.", "Native Git may remove its newly reserved destination if creation fails; no automatic cleanup follows."}}
	current, err := (gitAdapter{}).Observe(ctx, p.SourcePath, p.Expected.BaseSelector)
	if err != nil {
		return n, err
	}
	if !reflect.DeepEqual(current, p.Expected) {
		return n, failure("git-stale-plan", "Git source, base, references, registrations or safety configuration changed")
	}
	st, err := os.Lstat(p.Destination)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return n, failure("git-destination-refused", "task destination must be the reserved ordinary empty directory")
	}
	f, err := os.Open(p.Destination)
	if err != nil {
		return n, err
	}
	names, readErr := f.Readdirnames(1)
	f.Close()
	if len(names) != 0 || readErr != io.EOF {
		return n, failure("git-destination-refused", "task destination is occupied; foreign content is preserved")
	}
	if !validID(p.TaskID) {
		return n, failure("git-task-refused", "task identity is invalid")
	}
	protected, err := gitProtectedMetadata(p.Expected.Identity.RepositoryDir)
	if err != nil {
		return n, err
	}
	bin, err := gitExecutable()
	if err != nil {
		return n, err
	}
	n.Phase = "unknown"
	n.Effects = append(n.Effects, "native Git worktree creation attempted; destination and new registration may have changed")
	_, code, runErr := runSandboxed(ctx, bin, []string{"--no-replace-objects", "worktree", "add", "--detach", "--lock", "--reason", "stw:" + p.TaskID, "--", p.Destination, p.Expected.BaseCommit}, p.Expected.Identity.WorkspaceRoot, []string{p.Destination, filepath.Join(p.Expected.Identity.RepositoryDir, "worktrees")}, protected)
	verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	verified, verifyErr := (gitAdapter{}).Verify(verifyCtx, p)
	if verifyErr == nil {
		n.Identity = verified.Identity
		n.Phase = "registered"
		if runErr == nil && code == 0 && n.Identity.CommitID == p.Expected.BaseCommit && n.Identity.HeadRef == "" {
			n.Complete = true
			n.Phase = "materialized"
		}
		n.SourceUnchanged = verified.SourceUnchanged
	}
	if runErr != nil {
		return n, gitExecutionError(runErr)
	}
	if code != 0 {
		return n, failure("git-create-failed", "native Git creation failed; inspect the recorded destination and registration before manual recovery")
	}
	if verifyErr != nil {
		return n, verifyErr
	}
	if !n.Complete || !n.SourceUnchanged {
		return n, failure("git-outcome-incomplete", "Git creation completed without sufficient base or source preservation evidence")
	}
	return n, nil
}

func (gitAdapter) Verify(ctx context.Context, p MutationPlan) (NativeWorkspace, error) {
	n := NativeWorkspace{Phase: "unknown", Effects: []string{}, Limitations: []string{"Ownership verification is sampled; normal task commits and dirty files are preserved."}}
	id, err := gitNativeIdentity(ctx, p.Destination)
	if err != nil {
		return n, err
	}
	if id.WorkspaceRoot != p.Destination || id.RepositoryDir != p.Expected.Identity.RepositoryDir || id.GitDir == p.Expected.Identity.GitDir {
		return n, failure("git-foreign-workspace", "destination does not identify the planned native linked worktree")
	}
	parent := filepath.Join(id.RepositoryDir, "worktrees")
	if filepath.Dir(id.GitDir) != parent {
		return n, failure("git-foreign-workspace", "destination Git directory is not a native worktree registration")
	}
	lock, err := gitMetadata(filepath.Join(id.GitDir, "locked"))
	if err != nil || string(lock) != "stw:"+p.TaskID+"\n" {
		return n, failure("git-foreign-workspace", "native worktree lock does not match the recorded task identity")
	}
	gitdir, err := gitMetadata(filepath.Join(id.GitDir, "gitdir"))
	if err != nil || strings.TrimSuffix(string(gitdir), "\n") != filepath.Join(p.Destination, ".git") {
		return n, failure("git-foreign-workspace", "native worktree backlink does not match the recorded destination")
	}
	source, err := gitNativeIdentity(ctx, p.SourcePath)
	if err != nil {
		return n, err
	}
	n.Identity = id
	n.Phase = "materialized"
	n.Complete = true
	n.SourceUnchanged = reflect.DeepEqual(source, p.Expected.Identity)
	return n, nil
}
