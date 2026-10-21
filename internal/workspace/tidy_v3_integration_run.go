// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Native execution of the forge-free proofs (Spec 031 FR-008..FR-010, contract
// integration-proofs.md). Every command is a fixed typed argv under the
// planning session of the context (the constant configuration arguments and the
// private environment) with `git --attr-source=<tree>` pinned where attributes
// matter and with the private object directory of the run. A bound exceeded, a
// line outside its grammar, an unexpected exit, a timeout or a cancelled child
// is errIntegrationLimit for the affected class, never a partial proof.

var errIntegrationLimit = errors.New("integration proof command exceeds its bound")

// integrationLimitError is errIntegrationLimit with the bound that was hit: the
// name of the command or grammar, the unit (records, bytes, grammar, exit or
// command), the limit and the first value observed over it. A unit without a
// count (grammar, command) has limit and observed 0; exit has the exit code as
// observed. It matches errIntegrationLimit with errors.Is.
type integrationLimitError struct {
	Name, Unit      string
	Limit, Observed int64
}

func (e *integrationLimitError) Error() string        { return errIntegrationLimit.Error() }
func (e *integrationLimitError) Is(target error) bool { return target == errIntegrationLimit }

func limitHit(name, unit string, limit, observed int64) error {
	return &integrationLimitError{Name: name, Unit: unit, Limit: limit, Observed: observed}
}

// commandName is the git command and its leading options, for a limit message.
func commandName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	name := []string{args[0]}
	for _, a := range args[1:min(len(args), 4)] {
		if !strings.HasPrefix(a, "-") {
			break
		}
		name = append(name, a)
	}
	return strings.Join(name, " ")
}

// errIntegrationBudget is the per-tip or forge-free phase budget (FR-008).
var errIntegrationBudget = errors.New("integration proof time budget exhausted")

const (
	integrationCommandTimeout = 10 * time.Second
	integrationTipTimeout     = 20 * time.Second
	integrationPhaseTimeout   = 60 * time.Second
	integrationPatchStream    = 64 << 20
	integrationPathLimit      = 4096
	integrationAttributeLimit = 1 << 20
	integrationAttributeFiles = 64
)

// integrationEngine carries the seams of one planning or apply operation: the
// clock of the proof budget and the private object directory factory.
type integrationEngine struct {
	now            func() time.Time
	makeObjectDir  func() (string, error)
	commandTimeout time.Duration
	// trace, when set, sees the start of every class (tests assert the order).
	trace func(class string)
}

func defaultIntegrationEngine() integrationEngine {
	return integrationEngine{now: time.Now, commandTimeout: integrationCommandTimeout, makeObjectDir: func() (string, error) { return os.MkdirTemp("", "stw-proof-objects-") }}
}

// integrationRun is the state of one operation: the repository, the private
// object directory and the facts that hold for every tip.
type integrationRun struct {
	engine       integrationEngine
	bin, dir     string
	commonDir    string
	format       string
	width        int
	session      *planningSession
	objDir       string
	objErr       error
	notes        []V3ConfigNote
	hardStops    []string
	phaseStart   time.Time
	phaseStarted bool
	tipStart     time.Time
	// reporting lifts the proof time budget while the review report collects
	// its data: the report has its own bounds (FR-035).
	reporting bool
}

// beginTip starts the budget of one distinct tip (20 s) inside the forge-free
// phase (60 s, counters never reset). It reports the phase as exhausted.
func (r *integrationRun) beginTip() error {
	now := r.engine.now()
	if !r.phaseStarted {
		r.phaseStart, r.phaseStarted = now, true
	}
	r.tipStart = now
	return r.checkBudget(now)
}

// checkBudget reads the clock once and compares both budgets.
func (r *integrationRun) checkBudget(now time.Time) error {
	if !r.phaseStarted || r.reporting {
		return nil // no tip has started: nothing is budgeted yet
	}
	if now.Sub(r.phaseStart) >= integrationPhaseTimeout || now.Sub(r.tipStart) >= integrationTipTimeout {
		return errIntegrationBudget
	}
	return nil
}

func (r *integrationRun) traceClass(class string) {
	if r.engine.trace != nil {
		r.engine.trace(class)
	}
}

// begin prepares one run: hard stops of the history metadata, the private
// object directory and the configuration facts of the observation. The returned
// function removes the object directory on every exit path.
func (e integrationEngine) begin(ctx context.Context, id NativeIdentity, notes []V3ConfigNote) (*integrationRun, func(), error) {
	session := planningSessionFrom(ctx)
	if session == nil {
		return nil, func() {}, failure("planning-environment-unavailable", "Proofs run only under a planning session.")
	}
	bin, err := gitExecutable()
	if err != nil {
		return nil, func() {}, err
	}
	d := defaultIntegrationEngine()
	if e.now == nil {
		e.now = d.now
	}
	if e.makeObjectDir == nil {
		e.makeObjectDir = d.makeObjectDir
	}
	if e.commandTimeout == 0 {
		e.commandTimeout = d.commandTimeout
	}
	r := &integrationRun{engine: e, bin: bin, dir: id.WorkspaceRoot, commonDir: id.RepositoryDir, format: id.ObjectFormat, session: session, notes: notes}
	r.width = 40
	if r.format == "sha256" {
		r.width = 64
	}
	exists := func(parts ...string) bool {
		_, err := os.Lstat(filepath.Join(append([]string{r.commonDir}, parts...)...))
		return err == nil || !os.IsNotExist(err)
	}
	if exists("info", "grafts") || exists("shallow") {
		r.hardStops = append(r.hardStops, "history-override")
	}
	if exists("objects", "info", "alternates") {
		r.hardStops = append(r.hardStops, "unsupported-metadata")
	}
	// The private object directory is created by the first proof that needs it
	// and removed on every exit path.
	return r, func() {
		if r.objDir != "" {
			os.RemoveAll(r.objDir)
		}
	}, nil
}

// ensureObjectDir creates the private mode-0700 object directory once; failure
// is the review reason object-dir-unavailable for every tip.
func (r *integrationRun) ensureObjectDir() {
	if r.objDir != "" || r.objErr != nil {
		return
	}
	dir, err := r.engine.makeObjectDir()
	if err == nil {
		// The sandbox grants only canonical directories (a temporary directory
		// reached through a symbolic link would otherwise fail every proof).
		var canonical string
		if err = os.Chmod(dir, 0o700); err == nil {
			canonical, err = filepath.EvalSymlinks(dir)
		}
		if err != nil {
			os.RemoveAll(dir)
		} else {
			dir = canonical
		}
	}
	if err != nil {
		r.objErr = err
		return
	}
	r.objDir = dir
}

// command is one fixed native invocation.
type integrationCommand struct {
	attrTree string // pins --attr-source for attribute-dependent commands
	args     []string
	stdin    io.Reader
	limit    int64
	tolerate bool // the exit status is part of the result
	timeout  time.Duration
}

// run executes one command and hands its stdout to parse. The exit code is
// returned; an exit other than 0 is an error unless the command tolerates it.
// All bound violations are errIntegrationLimit.
func (r *integrationRun) run(ctx context.Context, c integrationCommand, parse func(io.Reader) error) (int, error) {
	if err := r.checkBudget(r.engine.now()); err != nil {
		return -1, err
	}
	timeout := c.timeout
	if timeout == 0 {
		timeout = r.engine.commandTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	streamCtx, stop := context.WithCancel(cmdCtx)
	defer stop()
	args := append([]string{}, c.args...)
	if c.attrTree != "" {
		args = append(r.session.attrSource(c.attrTree), args...)
	}
	cmd, err := prepareSandboxed(streamCtx, r.bin, args, r.dir, []string{r.objDir}, nil)
	if err != nil {
		return -1, r.classify(ctx, err)
	}
	cmd.Env = append(cmd.Env, "GIT_OBJECT_DIRECTORY="+r.objDir, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(r.commonDir, "objects"))
	stdin := c.stdin
	if stdin == nil {
		stdin = bytes.NewReader(nil)
	}
	_, code, err := executeNativeStreamExit(cmdCtx, stop, cmd, stdin, c.limit, parse, nil, c.tolerate)
	out := r.classify(ctx, err)
	if out == errIntegrationLimit {
		name := commandName(c.args)
		switch {
		case errors.Is(err, errNativeStreamLimit):
			out = limitHit(name, "bytes", c.limit, c.limit+1)
		case errors.Is(err, errNativeStreamCommandFailed):
			out = limitHit(name, "exit", 0, int64(code))
		default:
			out = limitHit(name, "command", 0, 0)
		}
	}
	return code, out
}

// classify maps an execution error: a cancelled operation is returned as is, a
// missing sandbox as its failure, every other defect of one command as the
// limit of the affected class.
func (r *integrationRun) classify(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errIntegrationBudget):
		return err
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errNativeSandboxUnavailable):
		return gitExecutionError(err)
	case errors.Is(err, errIntegrationLimit):
		return err
	}
	return errIntegrationLimit
}

// integrationSpec is one row of the command bounds table.
type integrationSpec struct {
	name       string
	maxRecords int
	maxBytes   [2]int64 // SHA-1 and SHA-256 widths
	sep        byte
	check      func(record []byte, width int) bool
}

func (s integrationSpec) bytes(width int) int64 {
	if width == 64 {
		return s.maxBytes[1]
	}
	return s.maxBytes[0]
}

func bothWidths(n int64) [2]int64 { return [2]int64{n, n} }

func hexExact(b []byte, n int) bool {
	if len(b) != n {
		return false
	}
	for _, c := range b {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func integrationPath(b []byte) bool {
	return len(b) >= 1 && len(b) <= integrationPathLimit && !bytes.ContainsRune(b, 0)
}

var (
	integrationCountPattern  = regexp.MustCompile(`^[0-9]{1,7}$`)
	integrationNumstatHeader = regexp.MustCompile(`^(?:[0-9]+|-)\t(?:[0-9]+|-)\t`)
	integrationLsTreeHeader  = regexp.MustCompile(`^[0-7]{6} (?:blob|commit|tree) `)
	integrationRawHeader     = regexp.MustCompile(`^:[0-7]{6} [0-7]{6} `)
)

// The command bounds table of contracts/integration-proofs.md. The patch-id
// byte bound is the contract's 5,120 records at the SHA-1 line width; the
// SHA-256 bound is the same 5,120 records at its own width (patch-ids have the
// width of the object format, measured on Git 2.56.0).
var (
	specMergeBase = integrationSpec{"merge-base --all", 8, bothWidths(520), '\n', func(r []byte, w int) bool { return hexExact(r, w) }}
	specRevParse  = integrationSpec{"rev-parse", 3, bothWidths(195), '\n', func(r []byte, w int) bool { return hexExact(r, w) }}
	specCount     = integrationSpec{"rev-list --count", 1, bothWidths(11), '\n', func(r []byte, _ int) bool { return integrationCountPattern.Match(r) }}
	specLog       = integrationSpec{"log %H %T %P", 1024, bothWidths(201728), '\n', func(r []byte, w int) bool {
		return len(r) == 3*w+2 && hexExact(r[:w], w) && r[w] == ' ' && hexExact(r[w+1:2*w+1], w) && r[2*w+1] == ' ' && hexExact(r[2*w+2:], w)
	}}
	specPatchID = integrationSpec{"patch-id", 5120, [2]int64{537600, 5120 * (2*64 + 2)}, '\n', func(r []byte, w int) bool {
		return len(r) == 2*w+1 && hexExact(r[:w], w) && r[w] == ' ' && hexExact(r[w+1:], w)
	}}
	specRaw = integrationSpec{"diff-tree --raw", 8192, bothWidths(34603008), '\n', func(r []byte, w int) bool {
		// :OLDMODE NEWMODE OID OID STATUS TAB PATH
		if !integrationRawHeader.Match(r) || len(r) < 1+6+1+6+1+w+1+w+1+1+1+1 {
			return false
		}
		o := 1 + 6 + 1 + 6 + 1
		return hexExact(r[o:o+w], w) && r[o+w] == ' ' && hexExact(r[o+w+1:o+2*w+1], w) && r[o+2*w+1] == ' ' && r[o+2*w+2] >= 'A' && r[o+2*w+2] <= 'Z' && r[o+2*w+3] == '\t' && integrationPath(r[o+2*w+4:])
	}}
	specNameOnly = integrationSpec{"diff-tree --name-only", 8192, bothWidths(34603008), 0, func(r []byte, _ int) bool { return integrationPath(r) }}
	specNumstat  = integrationSpec{"diff-tree --numstat", 8192, bothWidths(34603008), 0, func(r []byte, _ int) bool {
		h := integrationNumstatHeader.Find(r)
		return h != nil && integrationPath(r[len(h):])
	}}
	specLsTree = integrationSpec{"ls-tree -r", 16384, bothWidths(69206016), 0, func(r []byte, w int) bool {
		h := integrationLsTreeHeader.Find(r)
		if h == nil || len(r) < len(h)+w+2 {
			return false
		}
		o := len(h)
		return hexExact(r[o:o+w], w) && r[o+w] == '\t' && integrationPath(r[o+w+1:])
	}}
	specMergeTree = integrationSpec{"merge-tree", 1, bothWidths(4325376), '\n', func(r []byte, w int) bool { return hexExact(r, w) }}
)

// readIntegrationRecords parses one stream by its spec. The caller's accept
// sees each valid record; any defect is errIntegrationLimit.
func readIntegrationRecords(src io.Reader, s integrationSpec, width int, accept func(record []byte) error) error {
	reader := bufio.NewReaderSize(src, 64*1024)
	var total int64
	for count := 0; ; {
		record, err := reader.ReadSlice(s.sep)
		total += int64(len(record))
		if total > s.bytes(width) {
			return limitHit(s.name, "bytes", s.bytes(width), total)
		}
		if err == io.EOF && len(record) == 0 {
			return nil
		}
		if err != nil {
			return limitHit(s.name, "grammar", 0, 0) // truncated record, or longer than any grammar
		}
		if count++; count > s.maxRecords {
			return limitHit(s.name, "records", int64(s.maxRecords), int64(count))
		}
		record = record[:len(record)-1]
		if !s.check(record, width) {
			return limitHit(s.name, "grammar", 0, 0)
		}
		if err = accept(record); err != nil {
			return err
		}
	}
}

// records runs a command and returns its records as strings.
func (r *integrationRun) records(ctx context.Context, c integrationCommand, s integrationSpec) ([]string, int, error) {
	c.limit = s.bytes(r.width)
	var out []string
	code, err := r.run(ctx, c, func(src io.Reader) error {
		return readIntegrationRecords(src, s, r.width, func(rec []byte) error {
			out = append(out, string(rec))
			return nil
		})
	})
	return out, code, err
}

// lines is records for a command whose exit status must be 0.
func (r *integrationRun) lines(ctx context.Context, c integrationCommand, s integrationSpec) ([]string, error) {
	out, code, err := r.records(ctx, c, s)
	if err == nil && code != 0 {
		err = limitHit(commandName(c.args), "exit", 0, int64(code))
	}
	return out, err
}

func (r *integrationRun) count(ctx context.Context, args ...string) (int, error) {
	out, err := r.lines(ctx, integrationCommand{args: args}, specCount)
	if err != nil || len(out) != 1 {
		return 0, errIntegrationLimit
	}
	n := 0
	for _, c := range out[0] {
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// mergeBases returns every merge base of base and tip (none is exit 1).
func (r *integrationRun) mergeBases(ctx context.Context, base, tip string) ([]string, error) {
	out, code, err := r.records(ctx, integrationCommand{args: []string{"merge-base", "--all", base, tip}, tolerate: true}, specMergeBase)
	if err == nil && code != 0 && (code != 1 || len(out) != 0) {
		err = errIntegrationLimit
	}
	return out, err
}

// trees resolves the trees of tip, merge base and base in that order.
func (r *integrationRun) trees(ctx context.Context, tip, mergeBase, base string) (tipTree, mbTree, baseTree string, err error) {
	out, err := r.lines(ctx, integrationCommand{args: []string{"rev-parse", tip + "^{tree}", mergeBase + "^{tree}", base + "^{tree}"}}, specRevParse)
	if err != nil || len(out) != 3 {
		return "", "", "", errIntegrationLimit
	}
	return out[0], out[1], out[2], nil
}

// nulOrNewline helpers for stdin built from full OIDs and paths.
func joinRecords(records []string, sep byte) []byte {
	var b bytes.Buffer
	for _, r := range records {
		b.WriteString(r)
		b.WriteByte(sep)
	}
	return b.Bytes()
}
