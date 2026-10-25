// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"regexp"
	"strings"
)

// PullRequestState is one fresh read of the pull request, as the edge reports it. Repository
// names are exact "owner/name" strings; State is "open" or "closed" and Merged is separate, so a
// merged PR can never be mistaken for an open one. NodeID is the forge's global node id of the
// pull request (the REST node_id field): its source and the shape of the mutation response are
// not measured (research.md, Measured forge effects), so the grammar below is a conservative pin.
type PullRequestState struct {
	Number         int
	State          string
	Merged         bool
	Draft          bool
	NodeID         string
	BaseRepository string
	HeadRepository string
	HeadSHA        string
	BaseSHA        string
}

// ReadyIntent is what the durable intent hook records before the write: the repository, the PR
// and the exact head and base the Ready was evaluated at. It carries no credential and no node id.
type ReadyIntent struct {
	Repository string
	Number     int
	HeadSHA    string
	BaseSHA    string
}

// ReadyEdge is the narrow seam to the forge, the attribution gate and the journal. The pure
// package never fetches or writes; the caller supplies one implementation (a later task wires
// the bounded hosting write and the journal). Every method is called synchronously, in the order
// of ExecuteReady, and a method must not retry on its own.
type ReadyEdge interface {
	// Attribution applies the Spec 042 D5 gate and returns it as the attribution guard.
	Attribution() Guard
	// ReadPullRequest reads the one PR fresh.
	ReadPullRequest() (PullRequestState, error)
	// Evidence reads the Ready evidence (review, checks, rebaseability, scope, ownership) fresh for
	// the head and base of the PR state just read.
	Evidence(PullRequestState) (ReadyInput, error)
	// RecordIntent makes the intent durable. An error means it is not durable: no write follows.
	RecordIntent(ReadyIntent) error
	// WriteAvailable reports whether the Ready write capability is enabled and its credentials are
	// usable. It is asked before the intent, so no durable intent exists for a write that cannot be
	// sent. It must send nothing.
	WriteAvailable() bool
	// MarkReady sends the one Ready write for the node id. sent is false only when nothing was
	// sent (capability off, authentication, invalid id): that is a refusal. Once sent is true, a nil
	// error means the forge accepted it and any error means the outcome is unknown (the reply may
	// have been lost after the write).
	MarkReady(nodeID string) (sent bool, err error)
}

// ReadyRequest is the Ready action asked for: the PR, and the receipt of the earlier gate whose
// exact head and base the action may write at.
type ReadyRequest struct {
	Repository string
	Number     int
	Receipt    ReadyReceipt
}

// ReadyResult is the outcome of one Ready attempt. State is completed, refused or unknown. A
// refusal sent nothing (Attempted false). Observed is the last PR read, when there was one.
type ReadyResult struct {
	State        ActionState
	Reason       *Reason
	Detail       string
	Blockers     []Refusal
	AlreadyReady bool
	Attempted    bool
	Observed     *PullRequestState
}

// Status maps the outcome: complete, refused or unknown (exit 0 only for complete).
func (r ReadyResult) Status() Status {
	switch r.State {
	case StateCompleted:
		return StatusComplete
	case StateRefused:
		return StatusRefused
	}
	return StatusUnknown
}

// nodeIDGrammar is the pinned grammar of a pull request node id: URL-safe base64 characters
// (letters, digits, '_', '-', '=') only. It excludes quotes, braces, whitespace and every other
// character a query could be broken with. hosting.validNodeID repeats it; keep them equal.
var nodeIDGrammar = regexp.MustCompile(`^[A-Za-z0-9_=-]{4,128}$`)

func validRepoName(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && validName(owner) && validName(name)
}

func refusedReady(reason Reason, detail string) ReadyResult {
	return ReadyResult{State: StateRefused, Reason: reasonRef(reason), Detail: detail}
}

// stateMismatch checks a fresh PR read against the request: same PR, open, not merged, both
// sides in the one selected repository, head and base exactly the receipt's. It returns the
// refusal, or nil.
func stateMismatch(req ReadyRequest, pr PullRequestState) *Refusal {
	switch {
	case pr.Number != req.Number:
		return &Refusal{ReasonPlanStale, "pr-number"}
	case pr.State != "open" || pr.Merged:
		return &Refusal{ReasonPlanStale, "pr-not-open"}
	case pr.BaseRepository != req.Repository || pr.HeadRepository != req.Repository:
		return &Refusal{ReasonNotOwned, "repository"}
	case pr.HeadSHA != req.Receipt.Head:
		return &Refusal{ReasonHeadChanged, "head"}
	case pr.BaseSHA != req.Receipt.BaseSHA:
		return &Refusal{ReasonHeadChanged, "base"}
	}
	return nil
}

// d5Refusal checks the attribution guard the edge returned. Only a passing guard of the exact name
// counts; anything else refuses with its own reason, or attribution-unknown when it has none.
func d5Refusal(g Guard) *Refusal {
	if validGuard(g, "guard") != nil || g.Name != GuardAttribution {
		return &Refusal{ReasonAttributionUnknown, "d5"}
	}
	switch g.Result {
	case GuardPass:
		return nil
	case GuardFail, GuardUnknown:
		return &Refusal{g.Reason, "d5"}
	}
	return &Refusal{ReasonAttributionUnknown, "d5"}
}

// ExecuteReady performs one Ready attempt in the fixed order: D5, fresh PR read, state checks,
// Ready gate on fresh evidence, node id check, write availability, durable intent, the one write,
// fresh re-read. It writes nothing unless every earlier step passed and the intent is durable. An
// already-Ready PR is proven complete without a write. A write the edge never sent is a refusal
// (capability-unavailable), not unknown. A write whose reply is lost, or whose effect the re-read
// does not show, is unknown and is never retried here: reconciliation decides (Spec 016 forge
// effects).
func ExecuteReady(edge ReadyEdge, req ReadyRequest) ReadyResult {
	switch {
	case edge == nil:
		return refusedReady(ReasonCapabilityUnavailable, "edge-absent")
	case !validRepoName(req.Repository) || req.Number < 1:
		return refusedReady(ReasonPlanStale, "request-invalid")
	case !req.Receipt.Ready() || !isLowerHex(req.Receipt.Head, 40) || !isLowerHex(req.Receipt.BaseSHA, 40):
		return refusedReady(ReasonPlanStale, "receipt-not-ready")
	}
	if rf := d5Refusal(edge.Attribution()); rf != nil {
		return refusedReady(rf.Reason, rf.Detail)
	}
	pr, err := edge.ReadPullRequest()
	if err != nil {
		return refusedReady(ReasonPlanStale, "pr-unreadable")
	}
	if rf := stateMismatch(req, pr); rf != nil {
		return refusedReady(rf.Reason, rf.Detail)
	}
	if !pr.Draft {
		return ReadyResult{State: StateCompleted, AlreadyReady: true, Observed: &pr}
	}
	in, err := edge.Evidence(pr)
	if err != nil {
		return refusedReady(ReasonPlanStale, "evidence-unreadable")
	}
	// The gate is evaluated at the head and base of the fresh read, never at the caller's values.
	// D5 passed above; a failing or unknown attribution guard in the evidence still refuses.
	in.Head, in.BaseSHA = pr.HeadSHA, pr.BaseSHA
	in.Guards = append([]Guard{{Name: GuardAttribution, Result: GuardPass}}, in.Guards...)
	if receipt := EvaluateReady(in); !receipt.Ready() {
		first := receipt.Blockers[0]
		res := refusedReady(first.Reason, first.Detail)
		res.Blockers = append([]Refusal{}, receipt.Blockers...)
		return res
	}
	if !nodeIDGrammar.MatchString(pr.NodeID) {
		return refusedReady(ReasonCapabilityUnavailable, "node-id-invalid")
	}
	if !edge.WriteAvailable() {
		return refusedReady(ReasonCapabilityUnavailable, "write-unavailable")
	}
	if edge.RecordIntent(ReadyIntent{Repository: req.Repository, Number: req.Number, HeadSHA: pr.HeadSHA, BaseSHA: pr.BaseSHA}) != nil {
		return refusedReady(ReasonJournalFailed, "intent-not-durable")
	}
	sent, writeErr := edge.MarkReady(pr.NodeID)
	if !sent {
		// Nothing left the process: a refusal, not an unknown outcome. The intent stays recorded
		// and the caller's journal closes it as not executed.
		return refusedReady(ReasonCapabilityUnavailable, "write-not-sent")
	}
	res := ReadyResult{State: StateUnknown, Reason: reasonRef(ReasonOutcomeUnknown), Attempted: true}
	again, readErr := edge.ReadPullRequest()
	if readErr != nil {
		return res
	}
	res.Observed = &again
	if writeErr == nil && stateMismatch(req, again) == nil && !again.Draft {
		res.State, res.Reason = StateCompleted, nil
	}
	return res
}
