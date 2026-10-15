// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const jjTimeout = 10 * time.Second
const jjLimitation = "Stored working-copy state; disk edits and untracked files were not snapshotted. Freshness is unknown; observations are not transactional and concurrent or ABA changes may be missed."

var errJJOutput = errors.New("jj output limit")
var errJJChanged = errors.New("jj identity changed")
var errJJInvalid = errors.New("invalid jj operation identity")

type jjSession struct {
	ctx                  context.Context
	bin, sandbox, config string
	env                  []string
}

func inspectJJ(ctx context.Context, dir string, r Result) Result {
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	fail := func(code, msg string) Result {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "jj-timeout"
			msg = "jj inspection exceeded its ten-second deadline"
		} else if ctx.Err() != nil {
			code = "canceled"
			msg = "inspection canceled"
		}
		return r.fail(code, msg)
	}
	var observed JJRepository
	e := withJJSession(ctx, dir, r, func(s jjSession, root, metadata, repository string) error {
		var e error
		observed, e = observeJJ(ctx, root, metadata, repository, func(op string, args ...string) (string, error) {
			return s.read(dir, op, args...)
		})
		return e
	})
	if e != nil {
		var setup *jjSessionError
		if errors.As(e, &setup) {
			return fail(setup.code, setup.message)
		}
		if errors.Is(e, errJJOutput) {
			return fail("jj-output-limit", "jj output exceeded the inspection limit")
		}
		if errors.Is(e, errJJChanged) {
			return fail("changed-repository", "jj identity changed during inspection")
		}
		if errors.Is(e, errJJInvalid) {
			return fail("invalid-metadata", "jj returned an invalid operation identity")
		}
		return fail("jj-failed", "cannot read stored jj identity safely; check operation heads, workspace state and metadata without automatic repair")
	}
	r.Status = "ok"
	r.Repository = nil
	r.JJRepository = &observed
	r.Diagnostics = []Diagnostic{}
	return r
}

type jjSessionError struct{ code, message string }

func (e *jjSessionError) Error() string       { return e.message }
func jjSetupError(code, message string) error { return &jjSessionError{code, message} }

// withJJSession shares mandatory isolation and enforcement across read-only operations.
// Its callback cannot retain the private config: cleanup always runs before return.
func withJJSession(ctx context.Context, dir string, r Result, visit func(jjSession, string, string, string) error) error {
	metadata := r.Evidence[0].Location
	repository, e := resolveJJRepository(metadata)
	if e != nil {
		return jjSetupError("invalid-metadata", "cannot resolve valid jj repository metadata")
	}
	root := filepath.Dir(metadata)
	bin, e := exec.LookPath("jj")
	if e != nil {
		return jjSetupError("jj-unavailable", "jj executable is unavailable; install supported jj 0.45.1 or newer")
	}
	bin, e = filepath.Abs(bin)
	if e != nil {
		return jjSetupError("jj-unavailable", "cannot resolve jj executable")
	}
	bin, e = filepath.EvalSymlinks(bin)
	if e != nil {
		return jjSetupError("jj-unavailable", "cannot resolve jj executable")
	}
	tempBase := "/tmp"
	if runtime.GOOS == "darwin" {
		tempBase = "/private/tmp"
	}
	tempBase, e = filepath.EvalSymlinks(tempBase)
	if e != nil || within(root, tempBase) || within(repository, tempBase) {
		return jjSetupError("jj-sandbox-unavailable", "no private isolation directory is available outside the inspected repository")
	}
	config, e := os.MkdirTemp(tempBase, "stw-jj-inspect-")
	if e != nil {
		return jjSetupError("jj-sandbox-unavailable", "cannot prepare private jj isolation directory")
	}
	defer os.RemoveAll(config)
	if e := isolateJJConfig(metadata, repository, config); e != nil {
		return jjSetupError("invalid-metadata", "cannot safely isolate jj configuration metadata")
	}
	s := jjSession{ctx: ctx, bin: bin, config: config, env: jjEnvironment(config)}
	switch runtime.GOOS {
	case "darwin":
		s.sandbox = "/usr/bin/sandbox-exec"
	case "linux":
		s.sandbox, e = exec.LookPath("bwrap")
	default:
		e = errors.New("unsupported host")
	}
	if e != nil || s.sandbox == "" {
		return jjSetupError("jj-sandbox-unavailable", "native jj inspection requires usable read-only bubblewrap on Linux or sandbox-exec on macOS")
	}
	// Probe writes only in our private directory, never in the inspected target.
	probe := filepath.Join(config, "write-probe")
	_, code, e := s.execute(dir, "/bin/bash", []string{"--noprofile", "--norc", "-c", "if : > \"$1\"; then exit 42; else exit 0; fi", "stw-probe", probe})
	if errors.Is(e, errJJOutput) {
		return jjSetupError("jj-output-limit", "jj sandbox output exceeded the inspection limit")
	}
	if e != nil || code != 0 {
		return jjSetupError("jj-sandbox-unavailable", "read-only jj sandbox enforcement is unavailable; inspection was refused")
	}
	if _, e := os.Lstat(probe); !os.IsNotExist(e) {
		return jjSetupError("jj-sandbox-unavailable", "read-only jj sandbox probe failed; inspection was refused")
	}
	version, code, e := s.execute(dir, bin, []string{"--version"})
	if errors.Is(e, errJJOutput) {
		return jjSetupError("jj-output-limit", "jj version output exceeded the inspection limit")
	}
	if e != nil || code != 0 {
		return jjSetupError("jj-sandbox-unavailable", "jj cannot execute in the required read-only sandbox")
	}
	fields := strings.Fields(version)
	if len(fields) != 2 || fields[0] != "jj" || !supportedJJVersion(fields[1]) {
		return jjSetupError("jj-version-unsupported", "inspection requires stable jj 0.45.1 or newer with supported identity interfaces")
	}
	return visit(s, root, metadata, repository)
}

func (s jjSession) read(dir, op string, args ...string) (string, error) {
	v, code, e := s.query(dir, op, args...)
	if e != nil {
		return "", e
	}
	if code != 0 {
		return "", errors.New("native query failed")
	}
	if !utf8.ValidString(v) {
		return "", errors.New("invalid output encoding")
	}
	return v, nil
}

// observeJJ keeps native machine-interface sampling testable without executing repository data.
func observeJJ(ctx context.Context, root, metadata, repository string, query func(string, ...string) (string, error)) (JJRepository, error) {
	zero := JJRepository{}
	op, e := query("@", "operation", "log", "--limit", "1", "--no-graph", "--template", `json(self.id()) ++ "\n"`)
	if e != nil {
		return zero, e
	}
	operation, e := scalarRecord(op, 1)
	if e != nil || !identifier(operation[0], 128, "0123456789abcdef") {
		return zero, errJJInvalid
	}
	opID := operation[0]
	readIdentity := func() (JJRepository, error) {
		var j JJRepository
		out, e := query(opID, "workspace", "root")
		if e != nil {
			return j, e
		}
		workspace := terminated(out)
		if !utf8.ValidString(workspace) || workspace != root {
			return j, errors.New("workspace mismatch")
		}
		template := `if(stringify(self.root()) == ` + jjLiteral(root) + `, json(self.name()) ++ "\n" ++ json(self.root()) ++ "\n", "")`
		out, e = query(opID, "workspace", "list", "--template", template)
		if e != nil {
			return j, e
		}
		records, e := scalarRecord(out, 2)
		if e != nil || records[0] == "" || records[1] != root {
			return j, errors.New("invalid workspace identity")
		}
		out, e = query(opID, "log", "--revisions", "@", "--no-graph", "--template", `json(self.commit_id()) ++ "\n" ++ json(self.change_id()) ++ "\n"`)
		if e != nil {
			return j, e
		}
		ids, e := scalarRecord(out, 2)
		if e != nil || !identifier(ids[0], 40, "0123456789abcdef") || !identifier(ids[1], 32, "klmnopqrstuvwxyz") {
			return j, errors.New("invalid native identities")
		}
		j = JJRepository{WorkspaceRoot: root, MetadataDir: metadata, RepositoryDir: repository, WorkspaceName: records[0], CommitID: ids[0], ChangeID: ids[1], OperationID: opID, Observation: JJObservation{WorkingCopy: "stored-unsnapshotted", Limitation: jjLimitation}}
		return j, nil
	}
	first, e := readIdentity()
	if e != nil {
		return zero, e
	}
	second, e := readIdentity()
	if e != nil {
		return zero, e
	}
	final, e := query("@", "operation", "log", "--limit", "1", "--no-graph", "--template", `json(self.id()) ++ "\n"`)
	if e != nil {
		return zero, e
	}
	finalIDs, e := scalarRecord(final, 1)
	if e != nil || finalIDs[0] != opID || first != second {
		return zero, errJJChanged
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	return first, nil
}
func identifier(v string, n int, alphabet string) bool {
	return len(v) == n && strings.Trim(v, alphabet) == ""
}

// jj string literals accept these escapes, not JSON's HTML/unicode escapes.
func jjLiteral(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(s) + `"`
}
func within(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func scalarRecord(s string, n int) ([]string, error) {
	lines := strings.Split(s, "\n")
	if len(lines) != n+1 || lines[n] != "" {
		return nil, errors.New("invalid record count")
	}
	out := make([]string, n)
	for i := range out {
		if !strings.HasPrefix(lines[i], `"`) || !utf8.ValidString(lines[i]) {
			return nil, errors.New("invalid scalar record")
		}
		if e := json.Unmarshal([]byte(lines[i]), &out[i]); e != nil || !utf8.ValidString(out[i]) {
			return nil, errors.New("invalid scalar record")
		}
	}
	return out, nil
}
func supportedJJVersion(s string) bool {
	// Official builds append a Git hash, not a prerelease marker.
	version, hash, _ := strings.Cut(s, "-")
	if hash != "" && !identifier(hash, 40, "0123456789abcdef") {
		return false
	}
	p := strings.Split(version, ".")
	if len(p) != 3 {
		return false
	}
	major, e := strconv.Atoi(p[0])
	minor, f := strconv.Atoi(p[1])
	patch, g := strconv.Atoi(p[2])
	return e == nil && f == nil && g == nil && strconv.Itoa(major) == p[0] && strconv.Itoa(minor) == p[1] && strconv.Itoa(patch) == p[2] && major == 0 && (minor > 45 || minor == 45 && patch >= 1)
}
func jjEnvironment(config string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		switch upper {
		case "PATH", "LANG", "LC_ALL", "LC_CTYPE", "TZ":
			env = append(env, entry)
		}
	}
	return append(env, "JJ_CONFIG=", "HOME="+config, "USERPROFILE="+config, "XDG_CONFIG_HOME="+config, "XDG_CACHE_HOME="+config, "XDG_DATA_HOME="+config, "TMPDIR="+config, "TERM=dumb")
}
func (s jjSession) query(dir, op string, args ...string) (string, int, error) {
	prefix := []string{"--no-pager", "--color=never", "--ignore-working-copy", "--at-operation=" + op}
	return s.execute(dir, s.bin, append(prefix, args...))
}
func (s jjSession) execute(dir, bin string, args []string) (string, int, error) {
	var argv []string
	if runtime.GOOS == "darwin" {
		profile := `(version 1)(allow default)(deny file-write*)(deny network*)(deny process-exec)(allow process-exec (literal ` + strconv.Quote(bin) + `))`
		argv = append([]string{"-p", profile, bin}, args...)
	} else {
		argv = append([]string{"--ro-bind", "/", "/", "--unshare-net", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--", bin}, args...)
	}
	c := exec.CommandContext(s.ctx, s.sandbox, argv...)
	c.Dir = dir
	c.Env = s.env
	c.Stdin = strings.NewReader("")
	c.WaitDelay = time.Second
	var out, errout capture
	c.Stdout = &out
	c.Stderr = &errout
	e := c.Run()
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
func isolateJJConfig(metadata, repo, dest string) error {
	original := os.Getenv("XDG_CONFIG_HOME")
	if original == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		original = filepath.Join(home, ".config")
	}
	for _, item := range []struct{ base, id, legacy, group string }{{repo, "config-id", "config.toml", "repos"}, {metadata, "workspace-config-id", "workspace-config.toml", "workspaces"}} {
		if _, e := os.Lstat(filepath.Join(item.base, item.legacy)); !os.IsNotExist(e) {
			return errors.New("legacy configuration requires explicit migration")
		}
		b, e := metadataFile(filepath.Join(item.base, item.id))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		id := string(b)
		if !identifier(id, 20, "0123456789abcdefABCDEF") {
			return fmt.Errorf("invalid secure config identity")
		}
		source := filepath.Join(original, "jj", item.group, id, "metadata.binpb")
		b, e = metadataFile(source)
		if e != nil {
			return e
		}
		target := filepath.Join(dest, "jj", item.group, id)
		if e := os.MkdirAll(target, 0700); e != nil {
			return e
		}
		if e := os.WriteFile(filepath.Join(target, "metadata.binpb"), b, 0600); e != nil {
			return e
		}
	}
	return nil
}
