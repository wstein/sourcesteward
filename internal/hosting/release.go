// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrReleaseLimit identifies release evidence that cannot be enumerated completely.
var ErrReleaseLimit = errors.New("release hosting evidence exceeds its bounds")

// ReleaseEvidence contains only fields used by read-only release decisions.
type ReleaseEvidence struct {
	Runs          []ReleaseRun
	Releases      []ReleaseEntry
	Tags          []string
	MainSHA       string
	Rulesets      []ReleaseRuleset
	RulesetsKnown bool
	Network       []ReleaseRequest
}
type ReleaseRequest struct{ Method, Endpoint string }
type ReleaseRun struct {
	Workflow   string
	HeadSHA    string  `json:"head_sha"`
	Event      string  `json:"event"`
	Branch     string  `json:"head_branch"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
}
type ReleaseEntry struct {
	Tag    string         `json:"tag_name"`
	Draft  *bool          `json:"draft"`
	Assets []ReleaseAsset `json:"assets"`
}
type ReleaseAsset struct {
	Name string `json:"name"`
}

var releaseRoute = regexp.MustCompile(`\Arepos/[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*/(?:(?:releases|tags|rulesets)\?per_page=100&page=(?:[1-9]|10)|git/ref/heads/main|actions/workflows/[A-Za-z0-9_-][A-Za-z0-9_.-]*\.ya?ml/runs\?head_sha=[0-9a-f]{40}(?:[0-9a-f]{24})?&per_page=100&page=(?:[1-9]|10))\z`)

func releaseEndpoint(p string) bool {
	return releaseRoute.MatchString(p) && !strings.Contains(p, "/../")
}
func fixedReadEndpoint(p string) bool { return fixedEndpoint.MatchString(p) || releaseEndpoint(p) }

// ReadRelease acquires credentials only after callers establish native ownership and
// repository selection. No caller-supplied URL or method reaches the transport.
func ReadRelease(ctx context.Context, target string, repo Repository, commit string, workflows []string) (ReleaseEvidence, error) {
	return ReadReleaseWithTransport(ctx, target, repo, commit, workflows, nil)
}

// ReadReleaseWithTransport supplies the same bounded transport for in-process fixtures.
func ReadReleaseWithTransport(ctx context.Context, target string, repo Repository, commit string, workflows []string, rt http.RoundTripper) (ReleaseEvidence, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !validRepo(repo.Owner+"/"+repo.Name) || !validHost(repo.Host) || !releaseCommit(commit) {
		return ReleaseEvidence{}, errors.New("invalid release read selection")
	}
	for _, w := range workflows {
		if !releaseEndpoint("repos/" + repo.Owner + "/" + repo.Name + "/actions/workflows/" + w + "/runs?head_sha=" + commit + "&per_page=100&page=1") {
			return ReleaseEvidence{}, errors.New("invalid release workflow filename")
		}
	}
	api, cleanup, err := newGHWithTransport(ctx, target, repo.Host, rt)
	if err != nil {
		return ReleaseEvidence{}, err
	}
	defer cleanup()
	r, e := readRelease(ctx, api, repo.Owner+"/"+repo.Name, commit, workflows)
	if repo.Host != "github.com" {
		r.RulesetsKnown = false
	}
	return r, e
}
func releaseCommit(s string) bool {
	return (len(s) == 40 || len(s) == 64) && strings.Trim(s, "0123456789abcdef") == ""
}
func readRelease(ctx context.Context, api apiReader, repo, commit string, workflows []string) (r ReleaseEvidence, err error) {
	r.Runs = []ReleaseRun{}
	r.Releases = []ReleaseEntry{}
	r.Tags = []string{}
	r.Network = []ReleaseRequest{}
	get := func(ctx context.Context, p string) ([]byte, error) {
		r.Network = append(r.Network, ReleaseRequest{"GET", p})
		raw, err := api.get(ctx, p)
		if errors.Is(err, errBodyLimit) {
			err = ErrReleaseLimit
		}
		return raw, err
	}
	wrapped := apiReaderFunc(get)
	for _, w := range workflows {
		count := 0
		total := -1
		for page := 1; page <= maxPages; page++ {
			p := fmt.Sprintf("repos/%s/actions/workflows/%s/runs?head_sha=%s&per_page=100&page=%d", repo, w, commit, page)
			raw, e := wrapped.get(ctx, p)
			if e != nil {
				return r, e
			}
			var response struct {
				Total *int         `json:"total_count"`
				Runs  []ReleaseRun `json:"workflow_runs"`
			}
			if len(raw) > maxBytes {
				return r, ErrReleaseLimit
			}
			if e = releaseDecode(raw, &response); e != nil || response.Total == nil || response.Runs == nil || *response.Total < 0 || len(response.Runs) > 100 {
				return r, errors.New("invalid or truncated release workflow evidence")
			}
			if *response.Total > maxRecords {
				return r, ErrReleaseLimit
			}
			if total < 0 {
				total = *response.Total
			}
			if total != *response.Total {
				return r, errors.New("workflow total changed during pagination")
			}
			count += len(response.Runs)
			for _, run := range response.Runs {
				if !releaseCommit(run.HeadSHA) || run.Event == "" || run.Branch == "" || run.Status == "" {
					return r, errors.New("invalid release workflow evidence")
				}
				run.Workflow = w
				r.Runs = append(r.Runs, run)
			}
			if len(r.Runs) > maxRecords*max(1, len(workflows)) {
				return r, ErrReleaseLimit
			}
			if len(response.Runs) < 100 {
				if count != total {
					return r, ErrReleaseLimit
				}
				break
			}
			if page == maxPages {
				return r, ErrReleaseLimit
			}
		}
	}
	for page := 1; page <= maxPages; page++ {
		raw, e := wrapped.get(ctx, fmt.Sprintf("repos/%s/releases?per_page=100&page=%d", repo, page))
		if e != nil {
			return r, e
		}
		var rows []ReleaseEntry
		if len(raw) > maxBytes {
			return r, ErrReleaseLimit
		}
		if e = releaseDecode(raw, &rows); e != nil || rows == nil || len(rows) > 100 {
			return r, errors.New("invalid release evidence")
		}
		for _, row := range rows {
			if row.Tag == "" || row.Draft == nil || row.Assets == nil {
				return r, errors.New("incomplete release evidence")
			}
			for _, a := range row.Assets {
				if a.Name == "" {
					return r, errors.New("invalid release asset evidence")
				}
			}
		}
		r.Releases = append(r.Releases, rows...)
		if len(rows) < 100 {
			break
		}
		if page == maxPages {
			return r, ErrReleaseLimit
		}
	}
	r.Tags, err = releaseTags(ctx, wrapped, repo, nil)
	if err != nil {
		return r, err
	}
	raw, e := wrapped.get(ctx, "repos/"+repo+"/git/ref/heads/main")
	if e != nil {
		return r, e
	}
	var main struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if e = releaseDecode(raw, &main); e != nil || main.Ref != "refs/heads/main" || main.Object.Type != "commit" || !releaseCommit(main.Object.SHA) {
		return r, errors.New("invalid main ref evidence")
	}
	r.MainSHA = main.Object.SHA
	r.Rulesets, e = readReleaseRules(ctx, wrapped, repo)
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return r, e
	}
	r.RulesetsKnown = e == nil
	return r, nil
}
func releaseTags(ctx context.Context, api apiReader, repo string, _ []string) ([]string, error) {
	names := []string{}
	for page := 1; page <= maxPages; page++ {
		raw, e := api.get(ctx, fmt.Sprintf("repos/%s/tags?per_page=100&page=%d", repo, page))
		if e != nil {
			return nil, e
		}
		var rows []struct {
			Name string `json:"name"`
		}
		if len(raw) > maxBytes {
			return nil, ErrReleaseLimit
		}
		if e = releaseDecode(raw, &rows); e != nil || rows == nil || len(rows) > 100 {
			return nil, errors.New("invalid release tag evidence")
		}
		for _, row := range rows {
			if row.Name == "" {
				return nil, errors.New("invalid release tag name")
			}
			names = append(names, row.Name)
		}
		if len(rows) < 100 {
			return names, nil
		}
	}
	return nil, ErrReleaseLimit
}
func releaseDecode(raw []byte, out any) error {
	if len(raw) > maxBytes || !utf8.Valid(raw) {
		return errors.New("release response exceeds bounds")
	}
	if err := uniqueReleaseJSON(raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// ReleaseRuleset is advisory-only. The actual endpoint shape and permissions
// remain unverified; absent detail fields never establish protection.
type ReleaseRuleset struct {
	Target      string `json:"target"`
	Enforcement string `json:"enforcement"`
	Conditions  *struct {
		RefName *struct {
			Include []string `json:"include"`
			Exclude []string `json:"exclude"`
		} `json:"ref_name"`
	} `json:"conditions"`
	Rules []struct {
		Type string `json:"type"`
	} `json:"rules"`
}

func readReleaseRules(ctx context.Context, api apiReader, repo string) ([]ReleaseRuleset, error) {
	result := []ReleaseRuleset{}
	for page := 1; page <= maxPages; page++ {
		raw, err := api.get(ctx, fmt.Sprintf("repos/%s/rulesets?per_page=100&page=%d", repo, page))
		if err != nil {
			return nil, err
		}
		var rows []ReleaseRuleset
		if releaseDecode(raw, &rows) != nil || rows == nil || len(rows) > 100 {
			return nil, errors.New("ruleset advisory unavailable")
		}
		result = append(result, rows...)
		if len(rows) < 100 {
			return result, nil
		}
	}
	return nil, errors.New("ruleset advisory truncated")
}

func uniqueReleaseJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("release JSON nesting exceeds bounds")
		}
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate release JSON key")
				}
				seen[name] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid release JSON delimiter")
		}
		_, err = dec.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing release JSON")
	}
	return nil
}
