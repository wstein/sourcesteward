// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"unicode/utf8"
)

func newPinsSubmodule(e pinsEntry) PinsSubmodule {
	s := PinsSubmodule{
		Path: pinsDisplay(e.Path), PathEncoding: "utf8",
		Registration: PinsRegistration{GitlinkInHead: e.GitlinkInHead, GitlinkInIndex: e.GitlinkInIndex, GitmodulesEntry: e.GitmodulesEntry, Active: e.Active},
		URL:          PinsURL{Match: "unknown"},
		Pin:          PinsPin{HeadCommit: e.HeadCommit, IndexCommit: e.IndexCommit},
		Checkout: PinsCheckout{State: "uninitialized", RelationToPin: "unknown",
			Dirty: PinsDirty{Tracked: "unknown", Untracked: "unknown", Reason: "not evaluated"}},
		Publication:  PinsPublication{Status: "unknown", Reason: "not evaluated", ReachableFrom: []PinsRemoteRef{}},
		Tracking:     PinsTracking{Relation: "unknown"},
		FindingCodes: []string{},
	}
	if !e.PathUTF8 || !utf8.ValidString(e.Path) {
		s.PathEncoding = "escaped"
	}
	if e.Name != nil {
		n := pinsDisplay(*e.Name)
		s.Name = &n
	}
	s.Pin.StagedDiffers = e.IndexConflict || !pinsSameOID(e.HeadCommit, e.IndexCommit)
	return s
}

func pinsRelationPhrase(relation string) string {
	switch relation {
	case "ahead":
		return "ahead of"
	case "diverged":
		return "diverged from"
	case "unrelated":
		return "unrelated to"
	}
	return relation // behind
}

func pinsSameOID(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (a *pinsAuditor) examineOne(re pinsResolvedEntry, reg pinsRegistry) error {
	e := re.Entry
	sctx, cancel := context.WithTimeout(a.total, a.limits.PerSubmodule)
	defer cancel()
	srun := *a.run
	srun.ctx = sctx
	sub := newPinsSubmodule(e)
	var pending []pinsPendingFinding
	err := a.evidence(&srun, re, &sub, &pending)
	if err != nil {
		if msg, ok := a.limitText(err); ok {
			pending = append(pending, pinsPendingFinding{"pins-limit", "the evidence for this submodule is incomplete: " + msg})
		} else if a.parent.Err() != nil {
			return err // cancellation
		} else {
			// A repository that cannot be read is not audited: the submodule is refused, which is machine
			// readable and an error finding, and no field keeps an observation that was never made (spec
			// question: no dedicated code exists, submodule-path-unsafe is the nearest).
			msg := "the submodule repository could not be read: " + boundPinsMessage(err.Error())
			fresh := newPinsSubmodule(re.Entry)
			fresh.URL.Configured, fresh.URL.HasCredentials = sub.URL.Configured, sub.URL.HasCredentials
			fresh.Checkout.State = "refused"
			fresh.Refusal = &PinsRefusal{Code: "submodule-path-unsafe", Message: boundPinsMessage(msg)}
			fresh.Checkout.Dirty.Reason, fresh.Publication.Reason = "submodule refused", "submodule refused"
			sub = fresh
			pending = append(pending, pinsPendingFinding{"submodule-path-unsafe", msg})
		}
	}
	a.r.Submodules = append(a.r.Submodules, sub)
	path := sub.Path
	for _, p := range pending {
		a.add(p.code, &path, p.message)
	}
	return nil
}

func (a *pinsAuditor) evidence(run *pinsRunner, re pinsResolvedEntry, sub *PinsSubmodule, pending *[]pinsPendingFinding) error {
	e := re.Entry
	note := func(code, msg string) { *pending = append(*pending, pinsPendingFinding{code, msg}) }
	var info pinsURLInfo
	if e.URL != nil {
		info = pinsClassifyURL(*e.URL)
		sub.URL.Configured, sub.URL.HasCredentials = &info.Redacted, info.HasCredentials
		if info.HasCredentials {
			note("url-has-credentials", "the registered URL embeds credentials; they are not shown and the URL is never contacted")
		}
	}
	if e.OrphanGitlink {
		note("gitlink-without-entry", "the path is a gitlink with no .gitmodules entry")
	}
	if e.EntryWithoutGitlink {
		note("entry-without-gitlink", "the .gitmodules entry has no gitlink in HEAD or the index")
	}
	if sub.Pin.StagedDiffers {
		note("staged-pin-differs", "the staged pin differs from the pin recorded at HEAD; publication is evaluated for the HEAD pin (the staged pin only when HEAD has none), so the staged pin is not shown to be published")
	}
	refuse := func(code, msg string) {
		sub.Checkout.State = "refused"
		sub.Refusal = &PinsRefusal{Code: code, Message: boundPinsMessage(msg)}
		sub.Checkout.Dirty.Reason, sub.Publication.Reason = "submodule refused", "submodule refused"
		note(code, msg)
	}
	switch {
	case !e.PathUTF8:
		refuse("submodule-path-unsafe", "the submodule path is not valid UTF-8 and is shown escaped")
		return nil
	case re.Refusal != nil:
		refuse(re.Refusal.Code, re.Refusal.Message)
		return nil
	case re.Resolved.Uninitialized:
		sub.Checkout.Dirty.Reason, sub.Publication.Reason = "the submodule is not initialized", "the submodule is not initialized"
		note("submodule-uninitialized", "the submodule has no checkout; nothing was fetched or cloned")
		return nil
	}
	dirs := *re.Resolved.Dirs
	backend, err := pinsSubmoduleBackend(dirs)
	if err != nil {
		refuse("submodule-path-unsafe", "the submodule backend markers cannot be inspected safely")
		return nil
	}
	if backend != "" {
		refuse("submodule-unsupported-backend", "the submodule is managed by "+backend+" and is not audited")
		return nil
	}
	if err := a.safety.recheck(dirs); err != nil {
		if _, lim := err.(*pinsLimitError); lim {
			return err
		}
		refuse("submodule-path-unsafe", "the submodule directories or configuration changed after validation")
		return nil
	}
	repo := pinsGitRepo{GitDir: dirs.GitDir, WorkTree: dirs.WorkTree}
	sub.Checkout.State = "initialized"
	a.anyEval = true

	facts, err := pinsReadFacts(run, dirs)
	if err != nil {
		return err
	}
	head, err := pinsReadHead(run, repo)
	if err != nil {
		return err
	}
	sub.Checkout.HeadCommit, sub.Checkout.Branch, sub.Checkout.Detached = head.Commit, head.Branch, head.Detached
	a.dirs = append(a.dirs, pinsExamined{path: sub.Path, dirs: dirs})
	if head.Commit != nil {
		a.heads[sub.Path] = *head.Commit
	} else {
		a.heads[sub.Path] = ""
	}
	if head.Commit != nil && e.HeadCommit != nil {
		rel, err := pinsRelate(run, repo, facts, *head.Commit, *e.HeadCommit)
		if err != nil {
			return err
		}
		sub.Checkout.RelationToPin, sub.Checkout.Ahead, sub.Checkout.Behind, sub.Checkout.Capped = rel.Relation, rel.Ahead, rel.Behind, rel.Capped
		if rel.Relation != "same" && rel.Relation != "unknown" {
			note("checkout-differs-from-pin", "the checkout is "+pinsRelationPhrase(rel.Relation)+" the pin recorded at HEAD")
		}
	}

	m := pinsMatchRemote(info, facts.Remotes)
	sub.URL.Match, sub.URL.RemoteName, sub.URL.RemoteURL = m.Match, pinsDisplayPtr(m.RemoteName), pinsDisplayPtr(m.RemoteURL)
	if m.Match == "mismatch" {
		note("url-mismatch", "the registered URL differs from the URL of the submodule's own remote")
	}

	pin := e.HeadCommit
	if pin == nil {
		pin = e.IndexCommit
	}
	if pin == nil {
		sub.Publication.Reason = "no pin is recorded for this path"
	} else {
		pub, err := pinsPublish(run, repo, dirs, facts, *pin)
		if err != nil {
			return err
		}
		sub.Publication = PinsPublication{Status: pub.Status, Reason: pub.Reason, ReachableFrom: pub.Refs, Truncated: pub.Truncated, LastFetch: pub.LastFetch}
		switch {
		case pub.ObjectAbsent:
			note("pin-object-absent", "the submodule is initialized but the pinned commit is not in its repository")
		case pub.Status == "unreachable":
			note("pin-unpublished", "local evidence shows no remote-tracking ref contains the pin; this does not prove that no remote has it")
		}
	}

	remote := m.RemoteName
	if remote == nil {
		remote = pinsSoleRemote(facts)
	}
	tr, err := pinsTrack(run, repo, facts, pinsTrackInput{Pin: e.HeadCommit, Branch: e.Branch, SuperBranch: a.superBrnch, Remote: remote})
	if err != nil {
		return err
	}
	sub.Tracking = PinsTracking{Ref: tr.Ref, Relation: tr.Relation, Ahead: tr.Ahead, Behind: tr.Behind, Capped: tr.Capped}
	if tr.Relation == "behind" {
		note("pin-behind-tracking", "the pin is behind its tracking ref")
	}

	nested, err := pinsNestedPresent(run, repo, head)
	if err != nil {
		return err
	}
	if sub.Nested.Present = nested; nested {
		note("nested-submodules-present", "nested submodules exist and were not traversed")
	}

	dirty, err := pinsDirtyState(run, dirs, facts, head, a.limits.Dirty)
	if err != nil {
		return err
	}
	sub.Checkout.Dirty = dirty
	if dirty.Tracked == "present" || dirty.Untracked == "present" {
		note("submodule-dirty", "tracked changes or untracked files are present in the submodule")
	}
	return nil
}
