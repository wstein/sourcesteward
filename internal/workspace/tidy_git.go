// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type gitTidyAdapter struct{}
type gitTidyWorktree struct{ path, head, branch string }

func (gitTidyAdapter) Observe(ctx context.Context, path, base string) (TidyObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := TidyObservation{Candidates: []TidyCandidate{}, Limitations: []string{"Only native ancestry proves local integration; squash equivalence and forge evidence are unavailable and never inferred.", "State is sampled; external races and ABA changes are not transactionally excluded."}}
	expected, e := (gitAdapter{}).Observe(ctx, path, base)
	if e != nil {
		return out, e
	}
	if e := gitTidyAncestryMetadata(expected.Identity); e != nil {
		return out, e
	}
	bin, e := gitExecutable()
	if e != nil {
		return out, e
	}
	raw, e := gitRead(ctx, bin, path, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00%(objecttype)%00", "refs/heads/")
	if e != nil {
		return out, e
	}
	refs, e := parseGitTidyRefs(raw, expected.Identity.ObjectFormat)
	if e != nil {
		return out, e
	}
	raw, e = gitRead(ctx, bin, path, "worktree", "list", "--porcelain", "-z")
	if e != nil {
		return out, e
	}
	works, e := parseGitTidyWorktrees(raw, expected.Identity.ObjectFormat)
	if e != nil {
		return out, e
	}
	if len(refs)+len(works) > observationLimit {
		return out, failure("tidy-observation-limit", "Combined branch and worktree evidence exceeds 1024 records")
	}
	for _, ref := range refs {
		c := ref.candidate
		for _, w := range works {
			if w.branch == c.Name || w.head == c.CommitID {
				c.WorkspacePaths = append(c.WorkspacePaths, w.path)
			}
		}
		sort.Strings(c.WorkspacePaths)
		switch {
		case ref.symbolic:
			c.Reason = "symbolic-reference"
		case c.Name == base || c.CommitID == expected.BaseCommit:
			c.Reason = "selected-base"
		case len(c.WorkspacePaths) > 0:
			c.Reason = "workspace-used"
		default:
			_, code, err := runSandboxed(ctx, bin, []string{"--no-replace-objects", "merge-base", "--is-ancestor", c.CommitID, expected.BaseCommit}, path, nil, nil)
			if err != nil {
				return out, gitExecutionError(err)
			}
			switch code {
			case 0:
				c.Decision = "delete"
				c.Reason = "integrated-by-native-ancestry"
				c.Proof = "native-ancestry"
			case 1:
				c.Reason = "integration-not-proven"
			default:
				return out, failure("tidy-ancestry-unavailable", "Complete native Git ancestry proof is unavailable; no cleanup is authorized")
			}
		}
		out.Candidates = append(out.Candidates, c)
	}
	if e := gitTidyAncestryMetadata(expected.Identity); e != nil {
		return out, e
	}
	fresh, e := (gitAdapter{}).Observe(ctx, path, base)
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(fresh, expected) {
		return out, failure("tidy-stale-observation", "Git source, base, references, registrations or safety evidence changed during tidy observation")
	}
	out.Expected = expected
	return out, nil
}

type gitTidyRef struct {
	candidate TidyCandidate
	symbolic  bool
}

func parseGitTidyRefs(raw, format string) ([]gitTidyRef, error) {
	out := []gitTidyRef{}
	seen := map[string]bool{}
	for raw != "" {
		record, rest, ok := strings.Cut(raw, "\x00\n")
		if !ok || !utf8.ValidString(record) {
			return nil, failure("tidy-invalid-reference", "Malformed native branch evidence")
		}
		raw = rest
		fields := strings.Split(record, "\x00")
		if len(fields) != 4 || !strings.HasPrefix(fields[0], "refs/heads/") || len(fields[0]) <= len("refs/heads/") || !validText(fields[0]) || !fullGitOID(fields[1], format) || fields[3] != "commit" || seen[fields[0]] {
			return nil, failure("tidy-invalid-reference", "Native branch identities are incomplete or ambiguous")
		}
		seen[fields[0]] = true
		out = append(out, gitTidyRef{candidate: TidyCandidate{Kind: "branch", Name: fields[0], CommitID: fields[1], Decision: "retain", Reason: "integration-not-proven", Proof: "none", WorkspacePaths: []string{}, Limitations: []string{}}, symbolic: fields[2] != ""})
		if len(out) > observationLimit {
			return nil, failure("tidy-observation-limit", "Native branch evidence exceeds 1024 records")
		}
	}
	return out, nil
}

// Parse the complete NUL-delimited registration set. Missing directories and
// prunable/locked registrations still protect their recorded commit and branch.
// Unknown or incomplete native records never authorize reference cleanup.
func parseGitTidyWorktrees(raw, format string) ([]gitTidyWorktree, error) {
	bad := func() ([]gitTidyWorktree, error) {
		return nil, failure("tidy-invalid-registration", "Native worktree registrations are incomplete or ambiguous")
	}
	if raw == "" || !utf8.ValidString(raw) || !strings.HasSuffix(raw, "\x00\x00") {
		return bad()
	}
	records := strings.Split(strings.TrimSuffix(raw, "\x00\x00"), "\x00\x00")
	if len(records) > observationLimit {
		return bad()
	}
	out := []gitTidyWorktree{}
	paths := map[string]bool{}
	for _, record := range records {
		w := gitTidyWorktree{}
		seen := map[string]bool{}
		detached, bare := false, false
		for _, field := range strings.Split(record, "\x00") {
			key, value, hasValue := strings.Cut(field, " ")
			if seen[key] {
				return bad()
			}
			seen[key] = true
			switch key {
			case "worktree":
				if !hasValue || !filepath.IsAbs(value) || filepath.Clean(value) != value || !validText(value) {
					return bad()
				}
				w.path = value
			case "HEAD":
				if !hasValue || !fullGitOID(value, format) || strings.Trim(value, "0") == "" {
					return bad()
				}
				w.head = value
			case "branch":
				if !hasValue || !strings.HasPrefix(value, "refs/heads/") || len(value) <= len("refs/heads/") || !validText(value) {
					return bad()
				}
				w.branch = value
			case "detached":
				if hasValue {
					return bad()
				}
				detached = true
			case "bare":
				if hasValue {
					return bad()
				}
				bare = true
			case "locked", "prunable": // Native reasons are untrusted text and only qualify retention.
			default:
				return bad()
			}
		}
		if w.path == "" || paths[w.path] || bare || w.head == "" || detached == (w.branch != "") {
			return bad()
		}
		paths[w.path] = true
		out = append(out, w)
	}
	return out, nil
}

// Legacy grafts can fabricate ancestry without moving a reference. Replace refs
// are disabled by every native query; graft metadata is refused explicitly.
func gitTidyAncestryMetadata(id NativeIdentity) error {
	if _, e := os.Lstat(filepath.Join(id.RepositoryDir, "info", "grafts")); !os.IsNotExist(e) {
		return failure("tidy-grafts-refused", "Legacy Git graft metadata prevents trustworthy integration proof")
	}
	return nil
}
