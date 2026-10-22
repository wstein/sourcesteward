// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

// CodeTooManyReleases is the error code of a releases or tags listing that still has a next page
// after page 10 (1000 entries). The contract has no separate code for tags.
const CodeTooManyReleases = "too-many-releases"

// statusPerPage is the fixed page size of the releases and tags reads.
const statusPerPage = 100

// StatusRelease is one release as status needs it. CreatedAt is zero when GitHub gave none.
type StatusRelease struct {
	Draft     bool
	Name      string
	Tag       string
	URL       string
	Assets    int
	CreatedAt time.Time
}

// StatusTag is one tag with the commit it points at.
type StatusTag struct {
	Name string
	SHA  string
}

// ReleaseList reads every release of repo (drafts and published ones), following the Link header
// rel="next" to at most page 10; a next page after page 10 is CodeTooManyReleases. GitHub shows
// drafts only to a token with write access (see RepoInfo.DraftsVisibility), so an empty result of a
// token without it does not mean that there are no drafts.
func (r *WatchReader) ReleaseList(ctx context.Context, repo string) ([]StatusRelease, error) {
	var out []StatusRelease
	err := r.statusPages(ctx, KindReleases, repo, "releases", func(body []byte) (int, error) {
		var rows []struct {
			Draft     *bool             `json:"draft"`
			Name      *string           `json:"name"`
			Tag       string            `json:"tag_name"`
			CreatedAt *string           `json:"created_at"`
			HTMLURL   string            `json:"html_url"`
			Assets    []json.RawMessage `json:"assets"`
		}
		if releaseDecode(body, &rows) != nil || rows == nil {
			return 0, malformed("release page is not a JSON array of releases")
		}
		for _, row := range rows {
			if row.Draft == nil {
				return 0, malformed("release without a draft flag")
			}
			rel := StatusRelease{Draft: *row.Draft, Tag: row.Tag, URL: row.HTMLURL, Assets: len(row.Assets)}
			if row.Name != nil {
				rel.Name = *row.Name
			}
			if row.CreatedAt != nil {
				t, err := time.Parse(time.RFC3339, *row.CreatedAt)
				if err != nil {
					return 0, malformed("release with an unusable created_at")
				}
				rel.CreatedAt = t
			}
			out = append(out, rel)
		}
		return len(rows), nil
	})
	return out, err
}

// TagList reads every tag of repo with its commit, with the same paging as ReleaseList.
func (r *WatchReader) TagList(ctx context.Context, repo string) ([]StatusTag, error) {
	var out []StatusTag
	err := r.statusPages(ctx, KindTags, repo, "tags", func(body []byte) (int, error) {
		var rows []struct {
			Name   string `json:"name"`
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		if releaseDecode(body, &rows) != nil || rows == nil {
			return 0, malformed("tag page is not a JSON array of tags")
		}
		for _, row := range rows {
			if row.Name == "" || !validSHA(row.Commit.SHA) {
				return 0, malformed("tag without a name or a valid commit SHA")
			}
			out = append(out, StatusTag{Name: row.Name, SHA: row.Commit.SHA})
		}
		return len(rows), nil
	})
	return out, err
}

// statusPages reads `repos/REPO/COLLECTION?per_page=100&page=N` from page 1 on and hands each body
// to decode, which returns the number of rows it took. The next page is the one the Link header
// names and must be the current one plus one; the request is always built from the typed page
// number, never from the URL in the header. A page of more than 100 rows is malformed.
func (r *WatchReader) statusPages(ctx context.Context, kind WatchKind, repo, collection string, decode func([]byte) (int, error)) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}
	for page := 1; ; page++ {
		res, err := r.fetch(ctx, kind, base+"/"+collection+"?per_page="+strconv.Itoa(statusPerPage)+"&page="+strconv.Itoa(page), "", true)
		if err != nil {
			return err
		}
		n, err := decode(res.Body)
		if err != nil {
			return err
		}
		if n > statusPerPage {
			return malformed("page has more than 100 entries")
		}
		next, more := nextPageFromLink(res.Link)
		if res.Link != "" && !more && hasNextRel(res.Link) {
			return malformed("unusable Link header")
		}
		if !more {
			return nil
		}
		if next != page+1 {
			return malformed("Link next is not the following page")
		}
		if page >= maxPages {
			return &WatchError{Code: CodeTooManyReleases, Message: "more than 10 pages of " + collection}
		}
	}
}
