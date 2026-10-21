// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

type gitTidyAdapter struct{}
type gitTidyWorktree struct{ path, head, branch string }

func (gitTidyAdapter) Observe(ctx context.Context, path, base string) (TidyObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	out := TidyObservation{WorkspacePaths: []string{}, Candidates: []TidyCandidate{}, Limitations: []string{"Only native ancestry proves local integration; squash equivalence and forge evidence are unavailable and never inferred.", "State is sampled; external races and ABA changes are not transactionally excluded."}}
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
	allRefs, e := gitTidyReferenceMap(ctx, bin, path)
	if e != nil {
		return out, e
	}
	expected.ReferenceDigest = gitTidyReferenceDigest(allRefs)
	read := gitCollectionReaderFor(ctx, bin, path)
	refs, e := readGitTidyRefs(read, expected.Identity.ObjectFormat)
	if e != nil {
		return out, e
	}
	works, e := readGitTidyWorktrees(read, expected.Identity.ObjectFormat)
	if e != nil {
		return out, e
	}
	for _, w := range works {
		out.WorkspacePaths = append(out.WorkspacePaths, w.path)
	}
	sort.Strings(out.WorkspacePaths)
	out.WorkspacePathsComplete = true
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
	allRefs, e = gitTidyReferenceMap(ctx, bin, path)
	if e != nil {
		return out, e
	}
	fresh.ReferenceDigest = gitTidyReferenceDigest(allRefs)
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

// gitTidyRefParser accepts one for-each-ref record at a time.
type gitTidyRefParser struct {
	format string
	out    []gitTidyRef
	seen   map[string]bool
}

func newGitTidyRefParser(format string) *gitTidyRefParser {
	return &gitTidyRefParser{format: format, out: []gitTidyRef{}, seen: map[string]bool{}}
}

func (p *gitTidyRefParser) add(record []byte) error {
	text, ok := strings.CutSuffix(string(record), "\x00\n")
	if !ok || !utf8.ValidString(text) {
		return failure("tidy-invalid-reference", "Malformed native branch evidence")
	}
	fields := strings.Split(text, "\x00")
	if len(fields) != 4 || !strings.HasPrefix(fields[0], "refs/heads/") || len(fields[0]) <= len("refs/heads/") || !validText(fields[0]) || !fullGitOID(fields[1], p.format) || fields[3] != "commit" || p.seen[fields[0]] {
		return failure("tidy-invalid-reference", "Native branch identities are incomplete or ambiguous")
	}
	p.seen[fields[0]] = true
	p.out = append(p.out, gitTidyRef{candidate: TidyCandidate{Kind: "branch", Name: fields[0], CommitID: fields[1], Decision: "retain", Reason: "integration-not-proven", Proof: "none", WorkspacePaths: []string{}, Limitations: []string{}}, symbolic: fields[2] != ""})
	return nil
}

// A truncated final record is malformed evidence; bound refusals keep their code.
func readGitTidyRefs(read gitCollectionReader, format string) ([]gitTidyRef, error) {
	p := newGitTidyRefParser(format)
	if e := read(gitRecordLine, []string{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00%(objecttype)%00", "refs/heads/"}, p.add); e != nil {
		if errors.Is(e, io.ErrUnexpectedEOF) {
			return nil, failure("tidy-invalid-reference", "Malformed native branch evidence")
		}
		return nil, e
	}
	return p.out, nil
}

func readGitTidyWorktrees(read gitCollectionReader, format string) ([]gitTidyWorktree, error) {
	p := newGitTidyWorktreeParser(format)
	if e := read(gitRecordBlock, []string{"worktree", "list", "--porcelain", "-z"}, p.add); e != nil {
		return nil, gitTidyWorktreeStreamError(e)
	}
	return p.result()
}

func gitTidyWorktreeStreamError(e error) error {
	if errors.Is(e, io.ErrUnexpectedEOF) {
		return failure("tidy-invalid-registration", "Native worktree registrations are incomplete or ambiguous")
	}
	return e
}

// gitTidyWorktreeParser accepts the NUL-delimited registration set one record
// at a time. Missing directories and prunable/locked registrations still
// protect their recorded commit and branch. Unknown or incomplete native
// records never authorize reference cleanup.
type gitTidyWorktreeParser struct {
	format string
	out    []gitTidyWorktree
	paths  map[string]bool
}

func newGitTidyWorktreeParser(format string) *gitTidyWorktreeParser {
	return &gitTidyWorktreeParser{format: format, out: []gitTidyWorktree{}, paths: map[string]bool{}}
}

func (p *gitTidyWorktreeParser) result() ([]gitTidyWorktree, error) {
	if len(p.out) == 0 {
		return nil, failure("tidy-invalid-registration", "Native worktree registrations are incomplete or ambiguous")
	}
	return p.out, nil
}

func (p *gitTidyWorktreeParser) add(record []byte) error {
	bad := failure("tidy-invalid-registration", "Native worktree registrations are incomplete or ambiguous")
	text, ok := strings.CutSuffix(string(record), "\x00\x00")
	if !ok || !utf8.ValidString(text) {
		return bad
	}
	w := gitTidyWorktree{}
	seen := map[string]bool{}
	detached, bare := false, false
	for _, field := range strings.Split(text, "\x00") {
		key, value, hasValue := strings.Cut(field, " ")
		if seen[key] {
			return bad
		}
		seen[key] = true
		switch key {
		case "worktree":
			if !hasValue || !filepath.IsAbs(value) || filepath.Clean(value) != value || !validText(value) {
				return bad
			}
			w.path = value
		case "HEAD":
			if !hasValue || !fullGitOID(value, p.format) || strings.Trim(value, "0") == "" {
				return bad
			}
			w.head = value
		case "branch":
			if !hasValue || !strings.HasPrefix(value, "refs/heads/") || len(value) <= len("refs/heads/") || !validText(value) {
				return bad
			}
			w.branch = value
		case "detached":
			if hasValue {
				return bad
			}
			detached = true
		case "bare":
			if hasValue {
				return bad
			}
			bare = true
		case "locked", "prunable": // Native reasons are untrusted text and only qualify retention.
		default:
			return bad
		}
	}
	if w.path == "" || p.paths[w.path] || bare || w.head == "" || detached == (w.branch != "") {
		return bad
	}
	p.paths[w.path] = true
	p.out = append(p.out, w)
	return nil
}

// Legacy grafts can fabricate ancestry without moving a reference. Replace refs
// are disabled by every native query; graft metadata is refused explicitly.
func gitTidyAncestryMetadata(id NativeIdentity) error {
	if _, e := os.Lstat(filepath.Join(id.RepositoryDir, "info", "grafts")); !os.IsNotExist(e) {
		return failure("tidy-grafts-refused", "Legacy Git graft metadata prevents trustworthy integration proof")
	}
	return nil
}
