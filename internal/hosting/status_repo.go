// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
)

// This file and its two siblings (status_releases.go, status_unreleased.go) are the typed reads of
// the online `stw status` (Spec 037, work package B): the repository, its releases and its tags.
// They use the fixed request shapes every guard mode already admits; ReleaseEntry and the release
// verification are not touched.

// Kinds of the status reads, for the per-kind call accounting of the reader.
const (
	KindRepo     WatchKind = "repo"
	KindReleases WatchKind = "releases"
	KindTags     WatchKind = "tags"
)

// Values of the draft visibility of a repository (contract: `drafts_visibility`).
const (
	DraftsWriteAccess   = "write-access"
	DraftsNoWriteAccess = "no-write-access"
	DraftsUnknown       = "unknown"
)

// RepoInfo is what `repos/OWNER/NAME` tells status: the default branch, the page and the access of
// the token. A permission is nil when GitHub did not report it.
type RepoInfo struct {
	DefaultBranch string
	HTMLURL       string
	Admin         *bool
	Maintain      *bool
	Push          *bool
	Triage        *bool
	Pull          *bool
}

// DraftsVisibility says whether the token can see release drafts. GitHub shows drafts only to a
// token whose user has write access and does not say that it withheld any, so a missing or
// incomplete `permissions` object is `unknown`, never `no-write-access`: maintain, admin or push true
// is write access, all three reported false is no write access.
func (i RepoInfo) DraftsVisibility() string {
	known := true
	for _, p := range []*bool{i.Admin, i.Maintain, i.Push} {
		if p == nil {
			known = false
		} else if *p {
			return DraftsWriteAccess
		}
	}
	if known {
		return DraftsNoWriteAccess
	}
	return DraftsUnknown
}

// RepoInfo reads repos/OWNER/NAME. default_branch and html_url are required; permissions may be
// absent (a token without the information), which gives the `unknown` visibility.
func (r *WatchReader) RepoInfo(ctx context.Context, repo string) (RepoInfo, error) {
	base, err := repoPath(repo)
	if err != nil {
		return RepoInfo{}, err
	}
	res, err := r.fetch(ctx, KindRepo, base, "", false)
	if err != nil {
		return RepoInfo{}, err
	}
	var doc struct {
		DefaultBranch string `json:"default_branch"`
		HTMLURL       string `json:"html_url"`
		Permissions   *struct {
			Admin    *bool `json:"admin"`
			Maintain *bool `json:"maintain"`
			Push     *bool `json:"push"`
			Triage   *bool `json:"triage"`
			Pull     *bool `json:"pull"`
		} `json:"permissions"`
	}
	if releaseDecode(res.Body, &doc) != nil || doc.DefaultBranch == "" || doc.HTMLURL == "" {
		return RepoInfo{}, malformed("repository response is not usable")
	}
	info := RepoInfo{DefaultBranch: doc.DefaultBranch, HTMLURL: doc.HTMLURL}
	if p := doc.Permissions; p != nil {
		info.Admin, info.Maintain, info.Push, info.Triage, info.Pull = p.Admin, p.Maintain, p.Push, p.Triage, p.Pull
	}
	return info, nil
}
