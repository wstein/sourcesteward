// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
)

type tidyV3Ancestry func(context.Context, string, string) (bool, error)

func tidyV3Repository(r hosting.TidyRepositoryIdentity) V3Repository {
	return V3Repository{Host: r.Host, ID: r.ID, NodeID: r.NodeID, Name: r.FullName}
}

// proveTidyV3Commit keeps commit integration independent of reference identity.
// Callers enforce reference/workspace protection before selecting deletion. In
// particular, retirement preserves the real same-commit workspace evidence.
func proveTidyV3Commit(ctx context.Context, expected Snapshot, oid string, paths []string, repository *hosting.TidyRepositoryIdentity, evidence *hosting.TidyHeadEvidence, ancestry tidyV3Ancestry) (V3CommitProof, error) {
	out := V3CommitProof{OID: oid, Decision: "retain", Reason: "integration-not-proven", Proof: "none", WorkspacePaths: append([]string{}, paths...)}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if !v3Snapshot(expected, expected.Identity.WorkspaceRoot) || !v3OID(oid, expected.Identity.ObjectFormat) || !v3Paths(paths) || ancestry == nil {
		return out, failure("invalid-evidence", "Complete native commit/base and workspace identities are required.")
	}
	integrated, err := ancestry(ctx, oid, expected.BaseCommit)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return out, err
	}
	if integrated {
		out.Decision, out.Reason, out.Proof = "delete", "native-ancestry", "native-ancestry"
		out.Evidence = &V3ProofEvidence{Native: &V3AncestryProof{TargetOID: oid, BaseOID: expected.BaseCommit}}
		return out, nil
	}
	if repository == nil && evidence == nil {
		return out, nil
	}
	if repository == nil || evidence == nil || evidence.HeadOID != oid || !v3Repo(tidyV3Repository(*repository)) || expected.Identity.ObjectFormat != "sha1" {
		return out, failure("invalid-evidence", "Provider evidence does not bind the selected repository and exact native target.")
	}
	if evidence.PullRequest == nil {
		switch evidence.Reason {
		case "integration-not-proven", "pr-head-mismatch", "hosting-proof-unsupported", "pr-ambiguous":
			out.Reason = evidence.Reason
			return out, nil
		default:
			return out, failure("hosting-evidence-unavailable", "Provider observation is incomplete or has no recognized complete outcome.")
		}
	}
	pr := evidence.PullRequest
	if pr.Repository != *repository {
		return out, failure("evidence-changed", "Provider repository identity differs from the selected observation.")
	}
	if pr.HeadOID != oid {
		out.Reason = "pr-head-mismatch"
		return out, nil
	}
	if pr.HeadRepository == nil || pr.BaseRepository == nil || *pr.HeadRepository != repository.TidyRepositoryID || *pr.BaseRepository != repository.TidyRepositoryID {
		out.Reason = "hosting-proof-unsupported"
		return out, nil
	}
	if pr.State != "closed" || !pr.Merged || pr.MergedAt == nil || pr.Draft || pr.MergeOID == nil {
		return out, nil
	}
	g := V3MergeProof{Repository: tidyV3Repository(*repository), ID: pr.ID, NodeID: pr.NodeID, Number: int64(pr.Number), State: pr.State, Merged: pr.Merged, MergedAt: *pr.MergedAt, Draft: pr.Draft,
		Head: V3PRSide{Repository: tidyV3Repository(*repository), Ref: pr.HeadRef, OID: pr.HeadOID},
		Base: V3PRSide{Repository: tidyV3Repository(*repository), Ref: pr.BaseRef, OID: pr.BaseOID}, MergeOID: *pr.MergeOID, ChosenBaseOID: expected.BaseCommit}
	g.ObservationDigest, err = v3MergeDigest(g)
	if err != nil {
		return out, err
	}
	p := V3Plan{Expected: expected, Online: &V3OnlineSelection{Repository: g.Repository, PRHints: []V3PRHint{}, ObservationDigests: []string{g.ObservationDigest}}}
	if evidence.Reason != "github-merged-head" || !validV3Merge(g, oid, p) {
		return out, failure("hosting-evidence-unavailable", "The complete stable merged-head tuple is unavailable.")
	}
	// Original PR base ref/OID are immutable sampled facts, not the chosen
	// integration base. Only the reported result's native ancestry qualifies.
	integrated, err = ancestry(ctx, g.MergeOID, expected.BaseCommit)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return out, err
	}
	if !integrated {
		out.Reason = "pr-target-mismatch"
		return out, nil
	}
	out.Decision, out.Reason, out.Proof = "delete", "github-merged-head", "github-merged-head"
	out.Evidence = &V3ProofEvidence{GitHub: &g}
	return out, nil
}

// tidyV3NativeObservation retains complete native facts. Typed digests replace
// only the reference/registration channels; all original safety evidence stays.
type tidyV3NativeObservation struct {
	Expected    Snapshot
	Native      V3NativeState
	Config      string
	ConfigNotes []V3ConfigNote
}

func observeTidyV3Git(ctx context.Context, path, base string) (tidyV3NativeObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	out := tidyV3NativeObservation{}
	// Every native command of the observation runs under the planning
	// environment and constant argument list; apply re-evaluation joins it here.
	ctx, session, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return out, err
	}
	defer closeSession()
	var primaryBefore, primaryAfter gitPrimaryBinding
	before, err := observeGitWithPrimary(ctx, path, base, &primaryBefore)
	if err != nil {
		return out, err
	}
	notes := session.configNotes()
	if err = tidyV3AncestryMetadata(ctx, before.Identity); err != nil {
		return out, err
	}
	bin, err := gitExecutable()
	if err != nil {
		return out, err
	}
	read := gitCollectionReaderFor(ctx, bin, path)
	if out.Native.References, err = readTidyV3References(read, before.Identity.ObjectFormat); err != nil {
		return out, err
	}
	if out.Native.Registrations, err = readTidyV3Registrations(read, before.Identity.ObjectFormat); err != nil {
		return out, err
	}
	out.Native.ProtectedRoots = []string{}
	if len(out.Native.References)+len(out.Native.Registrations) > v3NativeFactsLimit {
		return out, failure("record-limit", "Complete native facts exceed 16384 records.")
	}
	out.Config, err = gitRead(withoutPlanningSession(ctx), bin, path, "config", "--null", "--list", "--includes", "--show-origin")
	if err != nil {
		return out, err
	}
	after, err := observeGitWithPrimary(ctx, path, base, &primaryAfter)
	if err != nil {
		return out, err
	}
	if err = tidyV3AncestryMetadata(ctx, after.Identity); err != nil {
		return out, err
	}
	if before != after || primaryBefore != primaryAfter || !reflect.DeepEqual(notes, session.configNotes()) {
		return out, failure("evidence-changed", "Native identity, base, complete references, registrations or safety changed during observation.")
	}
	out.Native.Registrations, err = projectTidyV3Primary(out.Native.Registrations, before.Identity, primaryBefore)
	if err != nil {
		return out, err
	}
	out.Expected = v3SnapshotWithState(before, out.Native)
	out.ConfigNotes = tidyV3ConfigNotesWithReplace(notes, out.Native.References)
	return out, nil
}

// tidyV3AncestryMetadata keeps Spec 029's refusal of graft metadata except in a
// local-reference planning context, where grafts are the hard stop
// history-override of every proof.
func tidyV3AncestryMetadata(ctx context.Context, id NativeIdentity) error {
	if historyHardStopsTolerated(ctx) {
		return nil
	}
	return gitTidyAncestryMetadata(id)
}

// refs/replace/* are never consulted (--no-replace-objects); say so once.
func tidyV3ConfigNotesWithReplace(notes []V3ConfigNote, refs []V3Reference) []V3ConfigNote {
	lines := []string{}
	for _, r := range refs {
		if strings.HasPrefix(r.Name, "refs/replace/") {
			lines = append(lines, r.Name+"\n"+r.OID)
		}
	}
	if len(lines) == 0 {
		return notes
	}
	h := sha256.Sum256([]byte(strings.Join(lines, "\x00")))
	notes = append(append([]V3ConfigNote{}, notes...), V3ConfigNote{KeyClass: "replace-refs-ignored", Disposition: v3ConfigClassPolicy["replace-refs-ignored"][0], Digest: hex.EncodeToString(h[:]), ApplyEffect: v3ConfigClassPolicy["replace-refs-ignored"][1]})
	sort.Slice(notes, func(i, j int) bool { return notes[i].KeyClass < notes[j].KeyClass })
	return notes
}

// tidyV3ReferenceParser accepts one for-each-ref record at a time and requires
// strictly ordered names.
type tidyV3ReferenceParser struct {
	format string
	last   string
	out    []V3Reference
}

func newTidyV3ReferenceParser(format string) *tidyV3ReferenceParser {
	return &tidyV3ReferenceParser{format: format, out: []V3Reference{}}
}

func (p *tidyV3ReferenceParser) add(record []byte) error {
	row, ok := strings.CutSuffix(string(record), "\x00\n")
	f := strings.Split(row, "\x00")
	if !ok || !utf8.ValidString(row) || len(f) != 4 || !v3Ref(f[0]) || f[0] <= p.last || !v3OID(f[1], p.format) || (f[2] != "" && !v3Ref(f[2])) {
		return failure("invalid-reference", "Complete ordered direct and symbolic native identities are required.")
	}
	if f[3] != "commit" && f[3] != "tag" && f[3] != "tree" && f[3] != "blob" || strings.HasPrefix(f[0], "refs/heads/") && f[3] != "commit" {
		return failure("invalid-reference", "Native reference object type is unsupported.")
	}
	p.out = append(p.out, V3Reference{Name: f[0], OID: f[1], Symbolic: f[2]})
	p.last = f[0]
	return nil
}

func readTidyV3References(read gitCollectionReader, format string) ([]V3Reference, error) {
	p := newTidyV3ReferenceParser(format)
	if e := read(gitRecordLine, []string{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00%(objecttype)%00"}, p.add); e != nil {
		if errors.Is(e, io.ErrUnexpectedEOF) {
			return nil, failure("invalid-reference", "Complete ordered direct and symbolic native identities are required.")
		}
		return nil, e
	}
	return p.out, nil
}

func readTidyV3Registrations(read gitCollectionReader, format string) ([]V3Registration, error) {
	p := newTidyV3RegistrationParser(format)
	if e := read(gitRecordBlock, []string{"worktree", "list", "--porcelain", "-z"}, p.add); e != nil {
		return nil, gitTidyWorktreeStreamError(e)
	}
	return p.result()
}

// tidyV3RegistrationParser validates each record with the v1 registration
// rules and keeps the typed v3 fields, sorted by path at the end.
type tidyV3RegistrationParser struct {
	validate *gitTidyWorktreeParser
	out      []V3Registration
}

func newTidyV3RegistrationParser(format string) *tidyV3RegistrationParser {
	return &tidyV3RegistrationParser{validate: newGitTidyWorktreeParser(format), out: []V3Registration{}}
}

func (p *tidyV3RegistrationParser) add(record []byte) error {
	if e := p.validate.add(record); e != nil {
		return e
	}
	r := V3Registration{}
	text, _ := strings.CutSuffix(string(record), "\x00\x00")
	for _, field := range strings.Split(text, "\x00") {
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			r.Path = value
		case "HEAD":
			r.HeadOID = value
		case "branch":
			r.HeadRef = value
		case "locked":
			r.Locked, r.LockReason = true, value
		case "prunable":
			r.Prunable = true
		case "bare":
			r.Bare = true
		}
	}
	p.out = append(p.out, r)
	return nil
}

func (p *tidyV3RegistrationParser) result() ([]V3Registration, error) {
	if _, e := p.validate.result(); e != nil {
		return nil, e
	}
	sort.Slice(p.out, func(i, j int) bool { return p.out[i].Path < p.out[j].Path })
	return p.out, nil
}

func tidyV3NativeAncestry(ctx context.Context, expected Snapshot, target, base string) (bool, error) {
	if !v3OID(target, expected.Identity.ObjectFormat) || !v3OID(base, expected.Identity.ObjectFormat) {
		return false, failure("invalid-evidence", "Ancestry requires full native object identities.")
	}
	bin, err := gitExecutable()
	if err != nil {
		return false, err
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return false, err
	}
	defer closeSession()
	_, code, err := runSandboxed(ctx, bin, []string{"--no-replace-objects", "merge-base", "--is-ancestor", target, base}, expected.Identity.WorkspaceRoot, nil, nil)
	if err != nil {
		return false, gitExecutionError(err)
	}
	if code > 1 {
		return false, failure("ancestry-unavailable", "Complete local ancestry is unavailable; no objects are fetched.")
	}
	return code == 0, nil
}

// Remote qualification binds exact configuration origins and bytes before auth.
// All URL rewriting refuses; native commands never use a remote transport.
func qualifyTidyV3Remote(o tidyV3NativeObservation, remote, override string) (string, string, error) {
	bad := func() (string, string, error) {
		return "", "", failure("hosting-proof-unsupported", "One canonical GitHub fetch URL and resolved configuration origins are required; URL rewrites refuse.")
	}
	if !validID(remote) || o.Expected.Identity.ObjectFormat != "sha1" || !strings.HasSuffix(o.Config, "\x00") {
		return bad()
	}
	fields := strings.Split(strings.TrimSuffix(o.Config, "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return bad()
	}
	urls, pushes := []string{}, []string{}
	for i := 0; i < len(fields); i += 2 {
		origin, row := fields[i], fields[i+1]
		if !strings.HasPrefix(origin, "file:") {
			return bad()
		}
		origin = strings.TrimPrefix(origin, "file:")
		if !filepath.IsAbs(origin) {
			origin = filepath.Join(o.Expected.Identity.WorkspaceRoot, origin)
		}
		canonical, err := filepath.EvalSymlinks(origin)
		if err != nil || canonical != origin {
			return bad()
		}
		key, value, _ := strings.Cut(row, "\n")
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "url.") && (strings.HasSuffix(lower, ".insteadof") || strings.HasSuffix(lower, ".pushinsteadof")) {
			return bad()
		}
		if strings.EqualFold(key, "remote."+remote+".url") {
			urls = append(urls, value)
		}
		if strings.EqualFold(key, "remote."+remote+".pushurl") {
			pushes = append(pushes, value)
		}
	}
	if len(urls) != 1 {
		return bad()
	}
	name := ""
	if strings.HasPrefix(urls[0], "git@github.com:") {
		name = strings.TrimPrefix(urls[0], "git@github.com:")
	} else {
		u, err := url.Parse(urls[0])
		if err != nil {
			return bad()
		}
		name = strings.TrimPrefix(u.Path, "/")
	}
	name = strings.TrimSuffix(name, ".git")
	if !v3Repo(V3Repository{Host: "github.com", ID: 1, NodeID: "selection", Name: name}) || !v3RemoteMatches(urls[0], name) || override != "" && !strings.EqualFold(override, name) {
		return bad()
	}
	for _, push := range pushes {
		if !v3RemoteMatches(push, name) {
			return bad()
		}
	}
	return urls[0], gitDigest(o.Config), nil
}

func sameTidyV3Observation(a, b tidyV3NativeObservation) bool { return reflect.DeepEqual(a, b) }

// tidyV3ProofSession exposes only complete repository/PR resampling; the
// native ancestry callback stays at the owning operation's sandbox boundary.
type tidyV3ProofSession interface {
	Repository() hosting.TidyRepositoryIdentity
	RevalidatePullRequest(context.Context, hosting.TidyPullRequest) error
}

// revalidateTidyV3Commit binds the entire stored tuple and native ancestry to
// the unchanged selected base. Retirement can use its actual workspace paths.
func revalidateTidyV3Commit(ctx context.Context, s tidyV3ProofSession, expected Snapshot, proof V3CommitProof, ancestry tidyV3Ancestry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := V3Plan{Expected: expected}
	if proof.Evidence != nil && proof.Evidence.GitHub != nil {
		g := proof.Evidence.GitHub
		p.Online = &V3OnlineSelection{Repository: g.Repository, PRHints: []V3PRHint{}, ObservationDigests: []string{g.ObservationDigest}}
	}
	if !v3Snapshot(expected, expected.Identity.WorkspaceRoot) || proof.Decision != "delete" || !validV3CommitProof(proof, p) || ancestry == nil {
		return failure("invalid-evidence", "Stored commit proof does not bind complete immutable native and provider facts.")
	}
	target := proof.OID
	if proof.Proof == "github-merged-head" {
		g := proof.Evidence.GitHub
		if s == nil || tidyV3Repository(s.Repository()) != g.Repository {
			return failure("evidence-changed", "Stored provider repository identity changed.")
		}
		if err := s.RevalidatePullRequest(ctx, tidyV3PullRequest(*g, s.Repository())); err != nil {
			return tidyV3HostingError(err)
		}
		target = g.MergeOID
	}
	integrated, err := ancestry(ctx, target, expected.BaseCommit)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !integrated {
		return failure("evidence-changed", "Stored integration result no longer has native ancestry into the selected base.")
	}
	return nil
}

func tidyV3PullRequest(g V3MergeProof, repo hosting.TidyRepositoryIdentity) hosting.TidyPullRequest {
	merged, result := g.MergedAt, g.MergeOID
	head := hosting.TidyRepositoryID{ID: g.Head.Repository.ID, NodeID: g.Head.Repository.NodeID, FullName: g.Head.Repository.Name}
	base := hosting.TidyRepositoryID{ID: g.Base.Repository.ID, NodeID: g.Base.Repository.NodeID, FullName: g.Base.Repository.Name}
	return hosting.TidyPullRequest{Repository: repo, ID: g.ID, NodeID: g.NodeID, Number: int(g.Number), State: g.State, Merged: g.Merged, MergedAt: &merged, Draft: g.Draft, HeadRepository: &head, HeadRef: g.Head.Ref, HeadOID: g.Head.OID, BaseRepository: &base, BaseRef: g.Base.Ref, BaseOID: g.Base.OID, MergeOID: &result}
}

func tidyV3HostingError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	code, message := "hosting-evidence-unavailable", "Complete bounded provider evidence is unavailable."
	switch {
	case errors.Is(err, hosting.ErrTidyChanged):
		code, message = "evidence-changed", "Provider identity or complete selected tuple changed; replan."
	case errors.Is(err, hosting.ErrTidyUnsupported):
		code, message = "hosting-proof-unsupported", "The selected native/provider identity is unsupported for online proof."
	case errors.Is(err, hosting.ErrTidyLimit):
		code, message = "hosting-evidence-limit", "Provider evidence exceeded the shared request, byte or deadline limit."
	}
	return errors.Join(failure(code, message), err)
}
