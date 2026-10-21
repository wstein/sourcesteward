// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// Reviewer decisions (FR-039..FR-041, contracts/decisions.md): evidence input
// for a plan, never authorization.

const (
	decisionFileLimit      = 1 << 20
	decisionCountLimit     = 1024
	decisionReasonLimit    = 1024
	decisionReviewerLimit  = 128
	reviewedIntegrated     = "reviewed-integrated"
	reviewedDrop           = "reviewed-drop"
	reviewedKeep           = "reviewed-keep"
	noteStaleDecision      = "stale-decision"
	noteDecisionNotApplied = "decision-not-applicable"
)

// V3DecisionRecord is one decision, as written by the reviewer and as recorded
// in the plan and the journal.
type V3DecisionRecord struct {
	Branch   string `json:"branch"`
	TipOID   string `json:"tip_oid"`
	BaseOID  string `json:"base_oid"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Reviewer string `json:"reviewer"`
	Kind     string `json:"kind"`
}

// V3DecisionFile is a parsed decision file and the digest of its bytes.
type V3DecisionFile struct {
	Decisions []V3DecisionRecord
	Digest    string
}

// V3ReviewNote records a decision that was ignored.
type V3ReviewNote struct {
	Code   string `json:"code"`
	Branch string `json:"branch"`
	Detail string `json:"detail"`
}

func reviewedProofTag(proof string) bool { return proof == reviewedIntegrated || proof == reviewedDrop }

func decisionError(path, where, why string) error {
	return failure("invalid-options", fmt.Sprintf("--decisions %q: %s%s.", clipText(path, 512), where, why))
}

type rawDecision struct {
	Branch   *string `json:"branch"`
	TipOID   *string `json:"tip_oid"`
	BaseOID  *string `json:"base_oid"`
	Decision *string `json:"decision"`
	Reason   *string `json:"reason"`
	Reviewer *string `json:"reviewer"`
	Kind     *string `json:"kind"`
}

func decisionOID(s string) bool {
	return (len(s) == 40 || len(s) == 64) && validAlphabet(s, len(s), "0123456789abcdef") && strings.Trim(s, "0") != ""
}

// ReadV3DecisionFile reads and validates a decision file: an absolute path to a
// regular file (no symbolic link) of at most 1 MiB with the fixed fields only.
func ReadV3DecisionFile(path string) (V3DecisionFile, error) {
	var out V3DecisionFile
	if !v3CanonicalPath(path) {
		return out, decisionError(path, "", "must be an absolute canonical path")
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return out, decisionError(path, "", "must be a readable regular file and not a symbolic link")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return out, decisionError(path, "", "must be a readable regular file and not a symbolic link")
	}
	defer f.Close()
	if fst, serr := f.Stat(); serr != nil || !fst.Mode().IsRegular() {
		return out, decisionError(path, "", "must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, decisionFileLimit+1))
	if err != nil || len(data) > decisionFileLimit {
		return out, decisionError(path, "", "must be readable and at most 1 MiB")
	}
	if err = strictDecisionJSON(data); err != nil {
		return out, decisionError(path, "", err.Error())
	}
	var top struct {
		SchemaVersion *int              `json:"schema_version"`
		Decisions     []json.RawMessage `json:"decisions"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&top); err != nil || top.SchemaVersion == nil || *top.SchemaVersion != 1 || top.Decisions == nil {
		return out, decisionError(path, "", "must be one JSON object with schema_version 1 and a decisions array and no other field")
	}
	if len(top.Decisions) > decisionCountLimit {
		return out, decisionError(path, "", "holds more than 1,024 decisions")
	}
	seen := map[string]bool{}
	for i, raw := range top.Decisions {
		where := fmt.Sprintf("decisions[%d]: ", i)
		var r rawDecision
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err = d.Decode(&r); err != nil || r.Branch == nil || r.TipOID == nil || r.BaseOID == nil || r.Decision == nil || r.Reason == nil || r.Reviewer == nil || r.Kind == nil {
			return out, decisionError(path, where, "needs exactly the fields branch, tip_oid, base_oid, decision, reason, reviewer and kind")
		}
		rec := V3DecisionRecord{Branch: *r.Branch, TipOID: *r.TipOID, BaseOID: *r.BaseOID, Decision: *r.Decision, Reason: *r.Reason, Reviewer: *r.Reviewer, Kind: *r.Kind}
		switch {
		case !v3Ref(rec.Branch) || !strings.HasPrefix(rec.Branch, "refs/heads/") || len(rec.Branch) == len("refs/heads/"):
			return out, decisionError(path, where, "branch must be a full ref under refs/heads/")
		case !decisionOID(rec.TipOID) || !decisionOID(rec.BaseOID):
			return out, decisionError(path, where, "tip_oid and base_oid must be full object ids")
		case !slices.Contains([]string{"integrated", "keep", "drop"}, rec.Decision):
			return out, decisionError(path, where, "decision must be integrated, keep or drop")
		case rec.Kind != "human" && rec.Kind != "ai":
			return out, decisionError(path, where, "kind must be human or ai")
		case !validText(rec.Reason) || len(rec.Reason) > decisionReasonLimit || rec.Decision != "keep" && rec.Reason == "":
			return out, decisionError(path, where, "reason must be text of at most 1,024 bytes and is required for integrated and drop")
		case !validText(rec.Reviewer) || rec.Reviewer == "" || len(rec.Reviewer) > decisionReviewerLimit:
			return out, decisionError(path, where, "reviewer must be non-empty text of at most 128 bytes")
		case seen[rec.Branch]:
			return out, decisionError(path, where, "names a branch that an earlier decision already names")
		}
		seen[rec.Branch] = true
		out.Decisions = append(out.Decisions, rec)
	}
	sum := sha256.Sum256(data)
	out.Digest = hex.EncodeToString(sum[:])
	return out, nil
}

func validV3DecisionRecord(r V3DecisionRecord) bool {
	return v3Ref(r.Branch) && strings.HasPrefix(r.Branch, "refs/heads/") && decisionOID(r.TipOID) && decisionOID(r.BaseOID) &&
		slices.Contains([]string{"integrated", "keep", "drop"}, r.Decision) && (r.Kind == "human" || r.Kind == "ai") &&
		validText(r.Reason) && len(r.Reason) <= decisionReasonLimit && (r.Decision == "keep" || r.Reason != "") &&
		validText(r.Reviewer) && r.Reviewer != "" && len(r.Reviewer) <= decisionReviewerLimit
}

func validV3ReviewNotes(notes []V3ReviewNote) bool {
	if notes == nil || len(notes) > decisionCountLimit {
		return false
	}
	for _, n := range notes {
		if (n.Code != noteStaleDecision && n.Code != noteDecisionNotApplied) || !validText(n.Branch) || !validText(n.Detail) || n.Detail == "" || len(n.Detail) > 4096 {
			return false
		}
	}
	return true
}

// decisionOf is the decision recorded in a candidate's evidence, if any.
func decisionOf(c V3Candidate) *V3DecisionRecord {
	if c.Evidence == nil || c.Evidence.Integration == nil {
		return nil
	}
	return c.Evidence.Integration.Decision
}

var decisionTopKeys = []string{"schema_version", "decisions"}
var decisionKeys = []string{"branch", "tip_oid", "base_oid", "decision", "reason", "reviewer", "kind"}

// strictDecisionJSON reads the file token by token before the typed decode:
// encoding/json lets the last of two equal keys win and matches key names
// without regard to case, so a human could read one decision while another is
// applied. Every key must be unique within its object and exactly one of the
// fixed lower-case names, and nothing may follow the one top-level object.
func strictDecisionJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	bad := func(why string) error { return fmt.Errorf("%s", why) }
	var value func(allowed []string, children func(key string) []string) error
	value = func(allowed []string, children func(key string) []string) error {
		tok, err := dec.Token()
		if err != nil {
			return bad("must be one JSON object with schema_version 1 and a decisions array and no other field")
		}
		switch d := tok.(type) {
		case json.Delim:
			if d == '[' {
				for dec.More() {
					if err = value(allowed, nil); err != nil {
						return err
					}
				}
				_, err = dec.Token()
				return err
			}
			seen := map[string]bool{}
			for dec.More() {
				k, err := dec.Token()
				key, ok := k.(string)
				if err != nil || !ok {
					return bad("has a malformed object")
				}
				if !slices.Contains(allowed, key) {
					return bad("has the field " + fmt.Sprintf("%q", clipText(key, 64)) + ", which is not one of the fixed lower-case field names")
				}
				if seen[key] {
					return bad("names the field " + fmt.Sprintf("%q", key) + " twice")
				}
				seen[key] = true
				var next []string
				if children != nil {
					next = children(key)
				}
				var nextChildren func(string) []string
				if err = value(next, nextChildren); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	err := value(decisionTopKeys, func(key string) []string {
		if key == "decisions" {
			return decisionKeys
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, err = dec.Token(); err != io.EOF {
		return bad("must hold exactly one JSON object")
	}
	return nil
}
