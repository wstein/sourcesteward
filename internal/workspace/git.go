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
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/inspection"
	"golang.org/x/text/unicode/norm"
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
	return observeGitWithPrimary(ctx, path, base, nil)
}

// The optional output exposes authority from the existing completed layout
// proof. Legacy callers keep exactly their original snapshot and admission.
func observeGitWithPrimary(ctx context.Context, path, base string, primary *gitPrimaryBinding) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
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
	safety, err := gitSafetyWithPrimary(ctx, bin, id, commit, primary)
	if err != nil {
		return Snapshot{}, err
	}
	refsDigest, err := gitReadRecordsDigest(ctx, bin, id.WorkspaceRoot, gitRecordLine, "for-each-ref", "--format=%(refname)%00%(objectname)%00")
	if err != nil {
		return Snapshot{}, err
	}
	registrationsDigest, err := gitReadRecordsDigest(ctx, bin, id.WorkspaceRoot, gitRecordBlock, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Identity: id, BaseSelector: base, BaseCommit: commit, ReferenceDigest: refsDigest, RegistrationDigest: registrationsDigest, SafetyDigest: safety}, nil
}

func gitSafety(ctx context.Context, bin string, id NativeIdentity, commit string) (string, error) {
	return gitSafetyWithPrimary(ctx, bin, id, commit, nil)
}
func gitSafetyWithPrimary(ctx context.Context, bin string, id NativeIdentity, commit string, primary *gitPrimaryBinding) (string, error) {
	config, layout, err := gitLayoutSafetyWithPrimary(ctx, bin, id, primary)
	if err != nil {
		return "", err
	}
	session := planningSessionFrom(ctx)
	creation := creationPolicyFrom(ctx)
	for _, entry := range strings.Split(strings.TrimSuffix(config, "\x00"), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		unsafe := strings.HasPrefix(key, "hook.") || strings.HasPrefix(key, "includeif.") || key == "core.hookspath" && !creation || key == "core.attributesfile" || key == "core.alternaterefscommand" || key == "extensions.worktreeconfig" || key == "extensions.partialclone" || key == "core.sparsecheckout" || key == "core.sparsecheckoutcone"
		// check-attr's textual sentinel values can also be literal driver names.
		// Refuse those definitions rather than mistake an executable literal for
		// absent/unset attributes. Case-insensitive refusal is conservative.
		unsafe = unsafe || strings.HasPrefix(key, "filter.unspecified.") || strings.HasPrefix(key, "filter.unset.") || strings.HasPrefix(key, "filter.set.")
		unsafe = unsafe || key == "core.fsmonitor" && !gitFalse(value) || !creation && strings.HasPrefix(key, "submodule.") || strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".promisor") && !gitFalse(value)
		if session != nil {
			// Planning (Spec 031 FR-020/FR-021): only hooksPath, fsmonitor and
			// submodule.* leave the refusal list; they are neutralized by the
			// constant arguments and recorded as plan notes below.
			unsafe = refusedPlanningKey(key, value)
		}
		if unsafe {
			return "", failure("git-unsafe-configuration", fmt.Sprintf("Git safety setting %q requires an explicit supported policy; configuration was preserved", gitSafetyRule(key)))
		}
	}
	// Relative/conditional hook paths are refused above; default hooks are shared.
	hooks, executableHook, err := gitHooksFingerprint(filepath.Join(id.RepositoryDir, "hooks"), session != nil)
	if err != nil {
		return "", err
	}
	unsupported := []string{filepath.Join(id.GitDir, "config.worktree"), filepath.Join(id.GitDir, "info", "sparse-checkout")}
	if !historyHardStopsTolerated(ctx) {
		// A planning context turns alternates into the hard stop
		// unsupported-metadata of the proofs; every other path refuses.
		unsupported = append(unsupported, filepath.Join(id.RepositoryDir, "objects", "info", "alternates"))
	}
	for _, p := range unsupported {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return "", failure("git-unsupported-metadata", "worktree-specific, sparse or alternate-object configuration is unsupported for task creation")
		}
	}
	digest := sha256.New()
	io.WriteString(digest, "layout-v1\x00"+layout+"\x00"+config+"\x00"+hooks+"\x00")
	paths, err := gitTreeEvidence(ctx, bin, id.WorkspaceRoot, commit, id.ObjectFormat, digest)
	if err != nil {
		return "", err
	}
	io.WriteString(digest, "\x00")
	if session != nil {
		// System, global and XDG configuration are null under the planning
		// environment, so only what Git would use there decides merge-driver.
		effective, err := gitRead(ctx, bin, id.WorkspaceRoot, "config", "--null", "--list", "--includes")
		if err != nil {
			return "", err
		}
		session.setNotes(planningConfigNotes(config, effective, hooks, executableHook, session.policy.scanInfoAttributes && infoAttributesActive(id.RepositoryDir), session.takeGitlinks()))
	}
	if err = gitFilterEvidence(ctx, bin, id.WorkspaceRoot, commit, paths, digest); err != nil {
		return "", err
	}
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() {
		return "", failure("git-unavailable", "Git executable identity is unavailable")
	}
	io.WriteString(digest, "\x00"+bin+"\x00"+fmt.Sprintf("%d:%d:%d", info.Mode(), info.Size(), info.ModTime().UnixNano()))
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func gitFalse(s string) bool { return s == "false" || s == "no" || s == "off" || s == "0" }

// Executable default hooks refuse unless planning merely records them: the
// fingerprint stays bound into the safety digest and apply refuses any effect
// that would run a hook.
func gitHooksFingerprint(dir string, allowExecutable bool) (string, bool, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return "absent", false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false, failure("git-hooks-refused", "cannot safely inspect the default Git hook directory")
	}
	f, err := os.Open(dir)
	if err != nil {
		return "", false, failure("git-hooks-refused", "cannot inspect Git hooks")
	}
	defer f.Close()
	entries, err := f.ReadDir(observationLimit + 1)
	if err != nil && err != io.EOF || len(entries) > observationLimit {
		return "", false, failure("git-hooks-refused", "Git hooks exceed the inspection bound")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var fingerprint strings.Builder
	executable := false
	for _, entry := range entries {
		st, e := entry.Info()
		runnable := e == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 && !strings.HasSuffix(entry.Name(), ".sample")
		if e != nil || !st.Mode().IsRegular() || runnable && !allowExecutable {
			return "", false, failure("git-hooks-refused", "default-hooks: executable hooks or unsupported hook directory entries prevent this effect; hooks are never silently skipped")
		}
		if st.Size() > 65536 {
			return "", false, failure("git-hooks-refused", "Git hook metadata exceeds the inspection bound")
		}
		executable = executable || runnable
		fmt.Fprintf(&fingerprint, "%s:%d:%d:%d\x00", entry.Name(), st.Mode(), st.Size(), st.ModTime().UnixNano())
	}
	return fingerprint.String(), executable, nil
}

// checkoutLeafLimit bounds the tracked leaf entries of a base checkout (tree
// validator and filter-path inventory). It is a fixed documented constant, not
// a configuration option, and is separate from observationLimit, which bounds
// results and records. Measured in the Spec 010 creation refusals amendment:
// 16,001 leaves observe in under 0.1 s and create in under 0.5 s, far inside
// the 10 s and 60 s deadlines. Blob, aggregate, record and stream limits stay.
const checkoutLeafLimit = 16384

type gitTreePathNode struct {
	name     string
	file     bool
	children map[string]*gitTreePathNode
}

type gitTreeValidator struct {
	// gitlinks admits submodule commits as data; planning never descends.
	gitlinks bool
	// limit is the leaf bound: checkoutLeafLimit, except for the stored-file
	// sample of retirement, which keeps its own 1,024-file bound.
	limit int
	count int
	total int64
	root  gitTreePathNode
}

func newGitTreeValidator() *gitTreeValidator {
	return &gitTreeValidator{limit: checkoutLeafLimit}
}
func (v *gitTreeValidator) add(entry string) error {
	v.count++
	if v.count > v.limit {
		return failure("git-checkout-limit", fmt.Sprintf("base checkout exceeds %d leaf entries", v.limit))
	}
	if len(entry)+1 > gitMetadataRecordLimit {
		return failure("git-checkout-limit", "base tree logical record exceeds 4224 bytes")
	}
	meta, path, ok := strings.Cut(entry, "\t")
	fields := strings.Fields(meta)
	gitlink := v.gitlinks && len(fields) == 4 && fields[0] == "160000" && fields[1] == "commit"
	if !ok || len(fields) != 4 || !gitlink && (fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755")) {
		return failure("git-tree-refused", "base contains unsupported tree objects, symlinks or gitlinks")
	}
	if len(path) > 4096 || path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\x00") || !utf8.ValidString(path) {
		return failure("git-tree-refused", "base contains an unsupported checkout path")
	}
	for _, component := range strings.Split(path, "/") {
		if component == "." || component == ".." || strings.EqualFold(component, ".git") || component == "" {
			return failure("git-tree-refused", "base contains an unsafe checkout path")
		}
	}
	components := strings.Split(path, "/")
	node := &v.root
	for i, component := range components {
		if node.children == nil {
			node.children = map[string]*gitTreePathNode{}
		}
		folded := strings.ToLower(norm.NFC.String(component))
		child, exists := node.children[folded]
		if exists && (child.name != component || child.file || i == len(components)-1) {
			return failure("git-tree-refused", "base contains aliased file or directory names")
		}
		if !exists {
			child = &gitTreePathNode{name: component}
			node.children[folded] = child
		}
		if i == len(components)-1 {
			child.file = true
		}
		node = child
	}

	if gitlink {
		return nil
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || size < 0 || size > 16*1024*1024 {
		return failure("git-checkout-limit", "base blob exceeds 16 MiB or has unavailable size")
	}
	v.total += size
	if v.total > 64*1024*1024 {
		return failure("git-checkout-limit", "base checkout exceeds 64 MiB")
	}
	return nil
}
func validateGitTree(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > gitMetadataStreamLimit || !strings.HasSuffix(raw, "\x00") {
		return failure("git-tree-refused", "base tree evidence is oversized or incomplete")
	}
	v := newGitTreeValidator()
	for _, entry := range strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00") {
		if e := v.add(entry); e != nil {
			return e
		}
	}
	return nil
}

func gitProtectedMetadata(common string) ([]string, bool, error) {
	dir := filepath.Join(common, "worktrees")
	st, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err = os.Mkdir(dir, 0700); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, false, failure("git-metadata-refused", "Git worktree metadata is not an ordinary directory")
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	entries, err := f.ReadDir(observationLimit + 1)
	if err != nil && err != io.EOF || len(entries) > observationLimit {
		return nil, false, failure("git-observation-limit", "existing worktree metadata exceeds the observation limit")
	}
	var roots []string
	for _, entry := range entries {
		st, e := entry.Info()
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, false, failure("git-metadata-refused", "existing worktree metadata contains unsupported entries")
		}
		roots = append(roots, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(roots)
	return roots, false, nil
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
	protected, metadataCreated, err := gitProtectedMetadata(p.Expected.Identity.RepositoryDir)
	if err != nil {
		return n, err
	}
	if metadataCreated {
		n.Effects = append(n.Effects, "created the native Git worktree metadata container")
	}
	bin, err := gitExecutable()
	if err != nil {
		return n, err
	}
	n.Phase = "unknown"
	n.Effects = append(n.Effects, "native Git worktree creation attempted; destination and new registration may have changed")
	_, code, runErr := runSandboxed(ctx, bin, append(append([]string{"--no-replace-objects"}, creationConfigArgs...), "worktree", "add", "--detach", "--lock", "--reason", "stw:"+p.TaskID, "--", p.Destination, p.Expected.BaseCommit), p.Expected.Identity.WorkspaceRoot, []string{p.Destination, filepath.Join(p.Expected.Identity.RepositoryDir, "worktrees")}, protected)
	// Cancellation after native start cannot imply rollback. Preserve the unknown
	// outcome rather than extending the total action deadline for reconciliation.
	if ctx.Err() != nil {
		return n, ctx.Err()
	}
	verified, verifyErr := (gitAdapter{}).Verify(ctx, p)
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
	bin, err := gitExecutable()
	if err != nil {
		return n, err
	}
	if _, _, err := gitLayoutSafety(ctx, bin, id); err != nil {
		return n, err
	}
	safety, err := gitSafety(ctx, bin, source, p.Expected.BaseCommit)
	if err != nil {
		return n, err
	}
	if safety != p.Expected.SafetyDigest {
		return n, failure("git-stale-plan", "Git source layout or safety configuration changed; replan before further effects")
	}
	n.Identity = id
	n.Phase = "materialized"
	n.Complete = true
	n.SourceUnchanged = reflect.DeepEqual(source, p.Expected.Identity)
	return n, nil
}

// Report the matched rule without disclosing config values or subsection names.
func gitSafetyRule(key string) string {
	switch {
	case strings.HasPrefix(key, "filter."):
		return "filter.<reserved-sentinel>.*"
	case strings.HasPrefix(key, "hook."):
		return "hook.*"
	case strings.HasPrefix(key, "includeif."):
		return "includeIf.*"
	case strings.HasPrefix(key, "submodule."):
		return "submodule.*"
	case strings.HasPrefix(key, "remote."):
		return "remote.*.promisor"
	default:
		// Refusal text uses the plan note class (core.hooksPath, core.fsmonitor).
		if class := planningConfigClass(key); class != "" {
			return class
		}
		return key
	}
}
