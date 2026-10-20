// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// planningConfigArgs is the one constant argument list (Spec 031 FR-019) that
// every planning command and every apply re-evaluation passes to Git. It
// disables hooks, fsmonitor, submodule recursion, automatic maintenance and
// rename or whitespace heuristics that would depend on caller configuration,
// then pins every key on which output parsing depends (policy output table).
var planningConfigArgs = []string{
	"-c", "core.hooksPath=" + os.DevNull,
	"-c", "core.fsmonitor=false",
	"-c", "submodule.recurse=false",
	"-c", "core.untrackedCache=false",
	"-c", "merge.renormalize=false",
	"-c", "merge.renames=true",
	"-c", "merge.directoryRenames=conflict",
	"-c", "merge.renameLimit=1000",
	"-c", "diff.renames=true",
	"-c", "diff.renameLimit=1000",
	"-c", "core.autocrlf=false",
	"-c", "gc.auto=0",
	"-c", "maintenance.auto=false",
	"-c", "protocol.ext.allow=never",
	// Output constants: a configured gpg.program would run for signed commits in
	// every `git log` if log.showSignature were inherited.
	"-c", "log.showSignature=false",
	"-c", "color.ui=never",
	"-c", "diff.noprefix=false",
	"-c", "diff.mnemonicPrefix=false",
	"-c", "diff.algorithm=myers",
	"-c", "diff.indentHeuristic=true",
	"-c", "diff.context=3",
	"-c", "diff.interHunkContext=0",
	"-c", "diff.colorMoved=false",
	"-c", "diff.wsErrorHighlight=none",
	"-c", "core.quotePath=true",
	"-c", "core.abbrev=auto",
	"-c", "i18n.logOutputEncoding=UTF-8",
	"-c", "log.decorate=no",
	"-c", "log.mailmap=false",
	"-c", "log.follow=false",
	"-c", "diff.orderFile=" + os.DevNull,
	"-c", "diff.relative=false",
	"-c", "diff.ignoreSubmodules=none",
}

// planningGitOptions precede the constant -c list.
var planningGitOptions = []string{"--no-optional-locks", "--no-replace-objects"}

// planningPolicy is replaceable only by tests, which mutate one property at a
// time to prove that the configuration matrix notices its removal.
type planningPolicy struct {
	arguments          []string
	environment        func(bin, home, xdg string) []string
	attrSource         func(tree string) []string
	scanInfoAttributes bool
}

var planningPolicyFactory = defaultPlanningPolicy

func defaultPlanningPolicy() planningPolicy {
	return planningPolicy{
		arguments:          append(append([]string{}, planningGitOptions...), planningConfigArgs...),
		environment:        planningEnvironment,
		attrSource:         func(tree string) []string { return []string{"--attr-source=" + tree} },
		scanInfoAttributes: true,
	}
}

// planningEnvironment is the sandbox environment without any inherited home or
// Git configuration location: private empty HOME and XDG_CONFIG_HOME, null
// global and system files, and no system configuration or attributes.
func planningEnvironment(bin, home, xdg string) []string {
	env := sandboxBaseEnvironment(bin)
	return append(env, "HOME="+home, "XDG_CONFIG_HOME="+xdg, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1")
}

// planningSession carries the policy, the process-owned private directories and
// the configuration notes of one planning operation through the context, so
// every existing typed read adopts it without changing its signature.
type planningSession struct {
	policy    planningPolicy
	home, xdg string
	mu        sync.Mutex
	notes     []V3ConfigNote
	gitlinks  []string
}
type planningSessionKey struct{}

func planningSessionFrom(ctx context.Context) *planningSession {
	s, _ := ctx.Value(planningSessionKey{}).(*planningSession)
	return s
}

// withPlanningSession starts, or joins, the planning session of ctx. The
// returned function removes the private directories of a session it started.
func withPlanningSession(ctx context.Context) (context.Context, *planningSession, func(), error) {
	if s := planningSessionFrom(ctx); s != nil {
		return ctx, s, func() {}, nil
	}
	home, err := os.MkdirTemp("", "stw-plan-home-")
	if err != nil {
		return ctx, nil, func() {}, failure("planning-environment-unavailable", "A private planning environment could not be created.")
	}
	xdg, err := os.MkdirTemp("", "stw-plan-xdg-")
	if err != nil {
		os.RemoveAll(home)
		return ctx, nil, func() {}, failure("planning-environment-unavailable", "A private planning environment could not be created.")
	}
	s := &planningSession{policy: planningPolicyFactory(), home: home, xdg: xdg}
	return context.WithValue(ctx, planningSessionKey{}, s), s, func() { os.RemoveAll(home); os.RemoveAll(xdg) }, nil
}

// withoutPlanningSession is for the one admission read, which must see global
// and system configuration with origins so that it can classify them.
func withoutPlanningSession(ctx context.Context) context.Context {
	return context.WithValue(ctx, planningSessionKey{}, (*planningSession)(nil))
}

func (s *planningSession) arguments(args []string) []string {
	return append(append([]string{}, s.policy.arguments...), args...)
}
func (s *planningSession) environment(bin string) []string {
	return s.policy.environment(bin, s.home, s.xdg)
}
func (s *planningSession) attrSource(tree string) []string { return s.policy.attrSource(tree) }

// noteGitlink records a gitlink path seen while the base tree was validated.
func (s *planningSession) noteGitlink(path string) {
	s.mu.Lock()
	s.gitlinks = append(s.gitlinks, path)
	s.mu.Unlock()
}
func (s *planningSession) takeGitlinks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string{}, s.gitlinks...)
	s.gitlinks = nil
	sort.Strings(out)
	return out
}
func (s *planningSession) setNotes(n []V3ConfigNote) {
	s.mu.Lock()
	s.notes = n
	s.mu.Unlock()
}
func (s *planningSession) configNotes() []V3ConfigNote {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.notes) == 0 {
		return nil
	}
	return append([]V3ConfigNote{}, s.notes...)
}

// V3ConfigNote records one configuration class that planning neutralized or
// that limits proofs (data model ConfigNote). It stores a digest of the
// sanitized key/value records, never the values.
type V3ConfigNote struct {
	KeyClass    string `json:"key_class"`
	Disposition string `json:"disposition"`
	Digest      string `json:"digest"`
	ApplyEffect string `json:"apply_effect"`
}

const (
	configNeutralized  = "neutralized"
	configReviewReason = "review-reason"
	applyNone          = "none"
	applyRefuses       = "refuses-apply"
)

// v3ConfigClassPolicy is the closed set of note classes with the disposition
// and apply effect of each. Planning accepts hooksPath, default hooks, fsmonitor,
// submodule configuration and gitlinks because every planning command is
// neutralized; the strict apply admission refuses all five, so each note says
// refuses-apply (an umbrella repository can plan but not apply).
var v3ConfigClassPolicy = map[string][2]string{
	"core.hooksPath": {configNeutralized, applyRefuses}, "default-hooks": {configNeutralized, applyRefuses},
	"core.fsmonitor": {configNeutralized, applyRefuses}, "submodule.*": {configNeutralized, applyRefuses}, "gitlinks": {configNeutralized, applyRefuses},
	"core.pager": {configNeutralized, applyNone}, "core.editor": {configNeutralized, applyNone},
	"core.sshCommand": {configNeutralized, applyNone}, "core.askPass": {configNeutralized, applyNone},
	"credential.*": {configNeutralized, applyNone}, "gpg.*": {configNeutralized, applyNone},
	"diff.external": {configNeutralized, applyNone}, "diff.*.textconv": {configNeutralized, applyNone},
	"merge-driver": {configReviewReason, applyNone}, "attribute-source": {configReviewReason, applyNone},
	"replace-refs-ignored": {configNeutralized, applyNone},
}

// planningConfigClass maps one lower-cased effective key to the class that
// planning neutralizes or notes, or "" when planning ignores the key.
func planningConfigClass(key string) string {
	switch {
	case key == "core.hookspath":
		return "core.hooksPath"
	case key == "core.fsmonitor":
		return "core.fsmonitor"
	case strings.HasPrefix(key, "submodule."):
		return "submodule.*"
	case key == "core.pager":
		return "core.pager"
	case key == "core.editor":
		return "core.editor"
	case key == "core.sshcommand":
		return "core.sshCommand"
	case key == "core.askpass":
		return "core.askPass"
	case strings.HasPrefix(key, "credential."):
		return "credential.*"
	case strings.HasPrefix(key, "gpg."):
		return "gpg.*"
	case key == "diff.external":
		return "diff.external"
	case strings.HasPrefix(key, "diff.") && strings.HasSuffix(key, ".textconv"):
		return "diff.*.textconv"
	case strings.HasPrefix(key, "merge.") && strings.HasSuffix(key, ".driver"):
		return "merge-driver"
	case strings.HasPrefix(key, "filter.") && (strings.HasSuffix(key, ".clean") || strings.HasSuffix(key, ".smudge") || strings.HasSuffix(key, ".process")):
		return "merge-driver"
	}
	return ""
}

// refusedPlanningKey lists the keys planning still refuses (policy table).
// core.hooksPath, core.fsmonitor and submodule.* are no longer among them.
func refusedPlanningKey(key, value string) bool {
	refused := strings.HasPrefix(key, "hook.") || strings.HasPrefix(key, "includeif.") || key == "core.attributesfile" || key == "core.alternaterefscommand" || key == "extensions.worktreeconfig" || key == "extensions.partialclone" || key == "core.sparsecheckout" || key == "core.sparsecheckoutcone"
	refused = refused || strings.HasPrefix(key, "filter.unspecified.") || strings.HasPrefix(key, "filter.unset.") || strings.HasPrefix(key, "filter.set.")
	return refused || strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".promisor") && !gitFalse(value)
}

// planningConfigNotes classifies the admission read. Keys of one class share
// one note whose digest covers the sorted sanitized key/value records.
func planningConfigNotes(config, hooks string, hasExecutableHook bool, infoAttributes bool, gitlinks []string) []V3ConfigNote {
	records := map[string][]string{}
	for _, entry := range strings.Split(strings.TrimSuffix(config, "\x00"), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		if class := planningConfigClass(key); class != "" {
			records[class] = append(records[class], key+"\n"+value)
		}
	}
	if hasExecutableHook {
		records["default-hooks"] = []string{hooks}
	}
	if len(gitlinks) > 0 {
		records["gitlinks"] = gitlinks
	}
	if infoAttributes {
		records["attribute-source"] = []string{"info/attributes"}
	}
	var notes []V3ConfigNote
	for class, lines := range records {
		sort.Strings(lines)
		h := sha256.Sum256([]byte(strings.Join(lines, "\x00")))
		policy := v3ConfigClassPolicy[class]
		notes = append(notes, V3ConfigNote{KeyClass: class, Disposition: policy[0], Digest: hex.EncodeToString(h[:]), ApplyEffect: policy[1]})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].KeyClass < notes[j].KeyClass })
	return notes
}

// validV3ConfigNotes checks a stored plan's notes: closed classes with their
// fixed disposition and apply effect, strictly sorted and unique.
func validV3ConfigNotes(notes []V3ConfigNote) bool {
	last := ""
	for _, n := range notes {
		policy, ok := v3ConfigClassPolicy[n.KeyClass]
		if !ok || n.KeyClass <= last || n.Disposition != policy[0] || n.ApplyEffect != policy[1] || !v3Hash(n.Digest) {
			return false
		}
		last = n.KeyClass
	}
	return true
}

const infoAttributesLimit = 1 << 20

// infoAttributesActive reports whether $GIT_DIR/info/attributes of the common
// git directory can change attribute evaluation: any content line, a symlink, a
// special file or more than 1 MiB (Git applies it even under --attr-source).
func infoAttributesActive(commonDir string) bool {
	path := filepath.Join(commonDir, "info", "attributes")
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || !st.Mode().IsRegular() || st.Size() > infoAttributesLimit {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, infoAttributesLimit+1))
	if err != nil || len(data) > infoAttributesLimit {
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func tidyV3HasConfigClass(notes []V3ConfigNote, class string) bool {
	for _, n := range notes {
		if n.KeyClass == class {
			return true
		}
	}
	return false
}

// tidyV3ApplyEffectGuard keeps Spec 029's refusal for the apply effects: the
// strict safety admission (no planning session) refuses configured and default
// hooks, hooksPath, fsmonitor and submodule configuration by class, before any
// journal or reference effect. Neutralization is planning-only (FR-022).
func tidyV3ApplyEffectGuard(ctx context.Context, p V3Plan) error {
	bin, err := gitExecutable()
	if err != nil {
		return err
	}
	_, err = gitSafety(withoutPlanningSession(ctx), bin, p.Expected.Identity, p.Expected.Identity.CommitID)
	return err
}

// submodulesInitialized reports initialized submodules (FR-021): a submodule
// url or active entry in the effective configuration, or populated
// $GIT_DIR/modules. submodule.recurse alone initializes nothing.
func submodulesInitialized(config, commonDir string) bool {
	for _, entry := range strings.Split(strings.TrimSuffix(config, "\x00"), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		if !strings.HasPrefix(key, "submodule.") || strings.Count(key, ".") < 2 {
			continue
		}
		if strings.HasSuffix(key, ".url") || strings.HasSuffix(key, ".active") && !gitFalse(value) {
			return true
		}
	}
	entries, err := os.ReadDir(filepath.Join(commonDir, "modules"))
	return err == nil && len(entries) > 0
}
