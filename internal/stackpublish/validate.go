// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	actionIDGrammar = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	taskIDGrammar   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	markerGrammar   = regexp.MustCompile(`^[A-Z0-9_]{1,64}$`)
	nameGrammar     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	hostGrammar     = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)
	componentRule   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

const maxRefBytes = 200

func invalid(code, field string) error { return &ValidationError{Code: code, Field: field} }

func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func absolutePath(s string) bool {
	return len(s) > 1 && s[0] == '/' && len(s) <= 4096 && !strings.ContainsRune(s, 0)
}

// validRef accepts a fully qualified refs/heads/... name whose components are ASCII letters,
// digits, dot, underscore and hyphen, with no leading dot or hyphen, "..", ".lock" suffix or
// control byte, at most 200 bytes.
func validRef(ref string) bool {
	rest, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok || len(ref) > maxRefBytes || rest == "" || strings.HasSuffix(ref, ".") {
		return false
	}
	for _, c := range strings.Split(rest, "/") {
		if c == "" || !componentRule.MatchString(c) || c[0] == '.' || c[0] == '-' || strings.Contains(c, "..") || strings.HasSuffix(c, ".lock") {
			return false
		}
	}
	return true
}

// checkSHA refuses everything but a full 40-hex SHA-1; a SHA-256 id is a distinct refusal and no
// id is ever truncated.
func checkSHA(field, v string) error {
	switch {
	case isLowerHex(v, 40):
		return nil
	case isLowerHex(v, 64):
		return invalid("unsupported-object-format", field)
	}
	return invalid("identity-invalid", field)
}

func needSHA(field string, v *string) error {
	if v == nil {
		return invalid("expected-missing", field)
	}
	return checkSHA(field, *v)
}

func needRef(field string, v *string) error {
	if v == nil || !validRef(*v) {
		return invalid("ref-invalid", field)
	}
	return nil
}

func needPR(field string, v *int) error {
	if v == nil || *v < 1 {
		return invalid("pull-request-invalid", field)
	}
	return nil
}

// checkText refuses a displayed text field that is empty, overlong or holds control bytes.
func checkText(field, v string, limit int) error {
	if v == "" || !utf8.ValidString(v) || strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return invalid("text-invalid", field)
	}
	if len(v) > limit {
		return invalid("bound-exceeded", field)
	}
	return nil
}

func validRepository(r *Repository, l Limits) error {
	switch r.Backend {
	case "git", "jj":
	default:
		return invalid("repository-invalid", "repository.backend")
	}
	if r.Forge != "github" {
		return invalid("repository-invalid", "repository.forge")
	}
	if !hostGrammar.MatchString(r.Host) {
		return invalid("repository-invalid", "repository.host")
	}
	if !validName(r.Owner) {
		return invalid("repository-invalid", "repository.owner")
	}
	if !validName(r.Name) {
		return invalid("repository-invalid", "repository.name")
	}
	for _, f := range []struct{ name, value string }{{"layout_identity", r.LayoutIdentity}, {"selection_provenance", r.SelectionProvenance}, {"default_source", r.DefaultSource}} {
		if err := checkText("repository."+f.name, f.value, l.DisplayBytes); err != nil {
			return err
		}
	}
	if !validRef(r.DefaultRef) {
		return invalid("repository-invalid", "repository.default_ref")
	}
	return checkSHA("repository.default_commit_id", r.DefaultCommitID)
}

// validName is the owner/repository name grammar of the hosting layer: ASCII letters, digits, "-",
// "_" and ".", at most 100 bytes, and neither "." nor "..".
func validName(s string) bool { return nameGrammar.MatchString(s) && s != "." && s != ".." }

func validReason(r Reason) bool {
	switch r {
	case ReasonOwnershipUnknown, ReasonNotOwned, ReasonAttributionMismatch, ReasonAttributionUnknown, ReasonPlanStale, ReasonAuthorizationMissing,
		ReasonPermissionUnknown, ReasonCapabilityUnavailable, ReasonRemoteChanged, ReasonHeadChanged, ReasonIdentityMismatch, ReasonIdentityConfigForeign,
		ReasonSignaturePolicyUnknown, ReasonReviewMissing, ReasonChecksUnknown, ReasonChecksNotGreen, ReasonRebaseableUnknown, ReasonScopeNotAccepted,
		ReasonNativeRewritePending, ReasonStackTooDeep, ReasonOutcomeUnknown, ReasonBoundExceeded, ReasonCancelled, ReasonJournalFailed:
		return true
	}
	return false
}

func validPlanKind(k ActionKind) error {
	switch k {
	case KindPushTopic, KindCreatePR, KindEditBody, KindRetarget, KindReady, KindNativeLink, KindNativeUnstack:
		return nil
	case KindCommentDeferred:
		return invalid("comment-deferred", "kind")
	}
	return invalid("unknown-kind", "kind")
}

// validTargetAndExpected checks the exact own target and expected full state of each kind.
func validTargetAndExpected(a Action, f string) error {
	t, e := a.Target, a.Expected
	switch a.Kind {
	case KindPushTopic:
		for _, err := range []error{needRef(f+".target.ref", t.Ref), needSHA(f+".expected.old_sha", e.OldSHA), needSHA(f+".expected.new_sha", e.NewSHA)} {
			if err != nil {
				return err
			}
		}
	case KindCreatePR:
		for _, err := range []error{needRef(f+".target.ref", t.Ref), needRef(f+".expected.planned_base", e.PlannedBase)} {
			if err != nil {
				return err
			}
		}
	case KindEditBody:
		if err := needPR(f+".target.pull_request", t.PullRequest); err != nil {
			return err
		}
		if e.BodyDigest == nil || !isLowerHex(*e.BodyDigest, 64) {
			return invalid("digest-invalid", f+".expected.body_digest")
		}
	case KindRetarget:
		for _, err := range []error{needPR(f+".target.pull_request", t.PullRequest), needSHA(f+".expected.head_sha", e.HeadSHA), needRef(f+".expected.base_ref", e.BaseRef), needRef(f+".expected.planned_base", e.PlannedBase)} {
			if err != nil {
				return err
			}
		}
		if *e.BaseRef == *e.PlannedBase {
			return invalid("retarget-no-change", f+".expected.planned_base")
		}
	case KindReady:
		for _, err := range []error{needPR(f+".target.pull_request", t.PullRequest), needSHA(f+".expected.head_sha", e.HeadSHA)} {
			if err != nil {
				return err
			}
		}
	case KindNativeLink, KindNativeUnstack:
		if len(t.PullRequests) < 2 {
			return invalid("pull-request-invalid", f+".target.pull_requests")
		}
		seen := map[int]bool{}
		for _, n := range t.PullRequests {
			if n < 1 || seen[n] {
				return invalid("pull-request-invalid", f+".target.pull_requests")
			}
			seen[n] = true
		}
		return needRef(f+".target.trunk_base", t.TrunkBase)
	}
	return nil
}

func checkAction(a Action, i int, l Limits, caps CapabilitySet, earlier map[string]bool) error {
	f := "actions[" + itoa(i) + "]"
	if !actionIDGrammar.MatchString(a.ID) {
		return invalid("action-id-invalid", f+".action_id")
	}
	if earlier[a.ID] {
		return invalid("duplicate-action", f+".action_id")
	}
	if err := validPlanKind(a.Kind); err != nil {
		return &ValidationError{Code: err.(*ValidationError).Code, Field: f + ".kind"}
	}
	if err := validTargetAndExpected(a, f); err != nil {
		return err
	}
	for _, e := range a.Effects {
		if err := checkText(f+".effects", e, l.DisplayBytes); err != nil {
			return err
		}
	}
	for _, dep := range a.DependsOn {
		if !earlier[dep] {
			return invalid("dependency-invalid", f+".depends_on")
		}
	}
	for _, g := range a.Guards {
		if err := validGuard(g, f+".guards"); err != nil {
			return err
		}
	}
	switch a.State {
	case StatePlanned:
		if a.Blocker != nil {
			return invalid("state-invalid", f+".blocker")
		}
		if !caps.Has(a.Kind) {
			return invalid("capability-unavailable", f+".kind")
		}
	case StateBlocked:
		if a.Blocker == nil || !validReason(*a.Blocker) {
			return invalid("state-invalid", f+".blocker")
		}
	default:
		return invalid("state-invalid", f+".state")
	}
	return nil
}

func validGuard(g Guard, f string) error {
	if err := checkText(f+".name", g.Name, 64); err != nil {
		return err
	}
	switch g.Result {
	case GuardPass, GuardNotApplicable:
		if g.Reason != "" {
			return invalid("guard-invalid", f)
		}
	case GuardFail, GuardUnknown:
		if !validReason(g.Reason) {
			return invalid("guard-invalid", f)
		}
	default:
		return invalid("guard-invalid", f)
	}
	return nil
}

func validPlan(d planDoc, caps CapabilitySet) error {
	if d.CreatedAt == "" {
		return invalid("created-at-invalid", "created_at")
	}
	if err := d.Limits.Validate(); err != nil {
		return err
	}
	l := d.Limits
	if !taskIDGrammar.MatchString(d.Task.TaskID) {
		return invalid("task-invalid", "task.task_id")
	}
	if err := validMode(d.Mode); err != nil {
		return err
	}
	if len(d.Actions) > l.Actions {
		return invalid("too-many-actions", "actions")
	}
	ids := map[string]bool{}
	for i, a := range d.Actions {
		if err := checkAction(a, i, l, caps, ids); err != nil {
			return err
		}
		ids[a.ID] = true
	}
	if err := validSelection(d, ids); err != nil {
		return err
	}
	if err := validBoundary(d, ids); err != nil {
		return err
	}
	switch {
	case d.Repository != nil:
		if err := validRepository(d.Repository, l); err != nil {
			return err
		}
	case d.Boundary.Complete || d.Error == nil || len(d.Actions) != 0:
		return invalid("repository-missing", "repository")
	}
	return nil
}

func validMode(m ModeSummary) error {
	if m.Derived != "human" && m.Derived != "agent" {
		return invalid("mode-invalid", "mode.derived")
	}
	for _, name := range m.Markers {
		if !markerGrammar.MatchString(name) {
			return invalid("mode-invalid", "mode.markers")
		}
	}
	return nil
}

func validSelection(d planDoc, ids map[string]bool) error {
	selected := map[string]bool{}
	for _, id := range d.Options.Selected {
		if selected[id] {
			return invalid("selection-duplicate", "options.selected")
		}
		selected[id] = true
	}
	if len(selected) != len(ids) {
		return invalid("selection-inconsistent", "options.selected")
	}
	for id := range ids {
		if !selected[id] {
			return invalid("selection-inconsistent", "options.selected")
		}
	}
	return nil
}

func validBoundary(d planDoc, ids map[string]bool) error {
	b := d.Boundary
	switch {
	case b.LocalReads < 0 || b.PrivateStateWrites < 0 || b.Requests < 0 || b.Requests > d.Limits.Requests:
		return invalid("boundary-inconsistent", "boundary")
	case !d.Options.Online && (b.NetworkRequested || b.NetworkAttempted || b.Requests != 0):
		return invalid("boundary-inconsistent", "boundary.network")
	case b.NetworkAttempted && !b.NetworkRequested, b.Requests > 0 && !b.NetworkAttempted:
		return invalid("boundary-inconsistent", "boundary.network")
	case !d.Saved && b.PrivateStateWrites != 0:
		return invalid("boundary-inconsistent", "boundary.private_state_writes")
	case b.Complete && (b.Cancelled || d.Error != nil):
		return invalid("boundary-inconsistent", "boundary.complete")
	}
	for _, bl := range d.Blockers {
		if !validReason(bl.Reason) || (bl.ActionID != nil && !ids[*bl.ActionID]) {
			return invalid("blocker-invalid", "blockers")
		}
	}
	if d.Error != nil {
		if !validReason(d.Error.Code) {
			return invalid("error-invalid", "error.code")
		}
		if err := checkText("error.message", d.Error.Message, d.Limits.ErrorBytes); err != nil {
			return err
		}
	}
	return nil
}
