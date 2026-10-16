// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/inspection"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type apiReader interface {
	get(context.Context, string) ([]byte, error)
}
type apiReaderFunc func(context.Context, string) ([]byte, error)

func (f apiReaderFunc) get(ctx context.Context, s string) ([]byte, error) { return f(ctx, s) }

type dependencies struct {
	remotes func(context.Context, string) inspection.RemoteResult
	api     func(context.Context, string, string) (apiReader, func(), error)
}

// Inspect samples local ownership and explicit GitHub read-only evidence.
func Inspect(ctx context.Context, path string, o Options) Result {
	return InspectWithTransport(ctx, path, o, nil)
}

// InspectWithTransport preserves native inspection and permits an in-process HTTP fixture.
// A nil transport selects the production HTTPS transport.
func InspectWithTransport(ctx context.Context, path string, o Options, rt http.RoundTripper) Result {
	return inspect(ctx, path, o, dependencies{remotes: func(ctx context.Context, path string) inspection.RemoteResult {
		return inspection.Remotes(ctx, inspection.Inspect(ctx, path))
	}, api: func(ctx context.Context, path, host string) (apiReader, func(), error) {
		return newGHWithTransport(ctx, path, host, rt)
	}})
}
func newResult(kind string) Result {
	return Result{SchemaVersion: 1, Kind: kind, Status: "error", Backend: "unknown", Candidates: []Candidate{}, Checks: []Check{}, Statuses: []CommitStatus{}, Diagnostics: []inspection.Diagnostic{}, ObservedAt: time.Now().UTC().Format(time.RFC3339), Boundary: Boundary{MaxPages: maxPages, MaxRecords: maxRecords, MaxBytes: maxBytes, TimeoutSeconds: 30, Limitations: []string{"Sampled evidence is not a transaction, required-check policy or merge eligibility.", "Only active native remotes are observed; repository existence is not verified by local resolution.", "Unsupported or unrelated-host remote URLs are excluded; credential-bearing URLs are never exposed."}}}
}
func (r *Result) fail(code, msg string) Result {
	r.Status = "error"
	r.Boundary.Complete = false
	r.Diagnostics = []inspection.Diagnostic{{Code: code, Message: msg}}
	return *r
}
func inspect(parent context.Context, path string, o Options, d dependencies) (r Result) {
	r = newResult(o.Kind)
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	defer func() {
		if ctx.Err() != nil {
			r = r.fail("hosting-canceled", "hosting inspection canceled or exceeded its thirty-second deadline")
		}
		encoded, e := json.Marshal(r)
		if e != nil || len(encoded) > maxBytes {
			r.Candidates = []Candidate{}
			r.Checks = []Check{}
			r.Statuses = []CommitStatus{}
			r.Identity = nil
			r.PullRequest = nil
			r.Repository = nil
			r = r.fail("hosting-output-limit", "hosting result exceeded its one MiB limit")
		}
		r.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	}()
	if err := ValidateOptions(o); err != nil {
		return r.fail("hosting-invalid-options", "invalid hosting inspection options")
	}
	if o.Host == "" {
		o.Host = "github.com"
	}
	o.Host = strings.ToLower(o.Host)
	remote := d.remotes(ctx, path)
	r.Identity = &remote.Identity
	r.Backend = remote.Identity.Backend
	if remote.Status != "ok" || remote.Identity.Status != "ok" {
		return r.fail("hosting-native-failed", "cannot establish complete native ownership and active remote evidence")
	}
	if o.RequireJJ && r.Backend != "jj" {
		return r.fail("hosting-jj-required", "gh jj requires a natively jj-owned workspace; use stw hosting for Git")
	}
	for _, m := range remote.Remotes {
		for _, raw := range append(append([]string{}, m.FetchURLs...), m.PushURLs...) {
			c, e := parseRemote(m.Name, raw, o.Host)
			if e != nil {
				if o.Repo != "" && o.Remote == "" {
					continue
				}
				if o.Remote != "" && o.Remote != m.Name {
					continue
				}
				return r.fail("hosting-invalid-remote", "cannot completely resolve selected-host remote URLs; select --repo explicitly")
			}
			if c != nil {
				r.Candidates = append(r.Candidates, *c)
			}
		}
	}
	selected, e := selectRepository(o, r.Candidates)
	if e != nil {
		return r.fail("hosting-repository-selection", e.Error())
	}
	r.Repository = selected
	if o.Kind == "repo" {
		r.Status = "ok"
		r.Boundary.Complete = true
		return r
	}
	target := remote.Identity.InputPath
	if remote.Identity.Repository != nil && remote.Identity.Repository.Bare {
		target = remote.Identity.Repository.GitDir
	}
	if remote.Identity.Repository != nil && remote.Identity.Repository.Worktree != nil {
		target = *remote.Identity.Repository.Worktree
	}
	if remote.Identity.JJRepository != nil {
		target = remote.Identity.JJRepository.WorkspaceRoot
	}
	api, cleanup, e := d.api(ctx, target, selected.Host)
	if e != nil {
		return r.fail("hosting-api-unavailable", e.Error())
	}
	defer cleanup()
	r.Boundary.Network = true
	pr, e := readPR(ctx, api, selected, o.Number)
	if e != nil {
		return r.fail("hosting-pr-failed", e.Error())
	}
	r.PullRequest = pr
	if o.Kind == "ci" {
		if e = readCI(ctx, api, &r); e != nil {
			return r.fail("hosting-ci-failed", e.Error())
		}
		latest, e := readPR(ctx, api, selected, o.Number)
		if e != nil {
			return r.fail("hosting-pr-revalidation", e.Error())
		}
		if latest.Number != pr.Number || latest.HeadSHA != pr.HeadSHA || latest.HeadRepository != pr.HeadRepository || latest.BaseRepository != pr.BaseRepository || latest.BaseSHA != pr.BaseSHA || latest.HeadRef != pr.HeadRef || latest.BaseRef != pr.BaseRef {
			return r.fail("hosting-pr-changed", "PR identity changed during CI inspection")
		}
	}
	r.Status = "ok"
	r.Boundary.Complete = true
	return r
}
func fullSHA(s string) bool { return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == "" }

type apiRef struct {
	SHA  *string `json:"sha"`
	Ref  *string `json:"ref"`
	Repo *struct {
		FullName *string `json:"full_name"`
	} `json:"repo"`
}

func readPR(ctx context.Context, api apiReader, repo *Repository, number int) (*PullRequest, error) {
	raw, e := api.get(ctx, fmt.Sprintf("repos/%s/%s/pulls/%d", repo.Owner, repo.Name, number))
	if e != nil {
		return nil, e
	}
	var p struct {
		Number *int    `json:"number"`
		Title  *string `json:"title"`
		URL    *string `json:"html_url"`
		State  *string `json:"state"`
		Draft  *bool   `json:"draft"`
		Head   *apiRef `json:"head"`
		Base   *apiRef `json:"base"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &p) != nil || p.Number == nil || *p.Number != number || p.Title == nil || p.URL == nil || p.State == nil || p.Draft == nil || p.Head == nil || p.Base == nil {
		return nil, errors.New("PR response is missing required evidence")
	}
	for _, ref := range []*apiRef{p.Head, p.Base} {
		if ref.SHA == nil || !fullSHA(*ref.SHA) || ref.Ref == nil || *ref.Ref == "" || ref.Repo == nil || ref.Repo.FullName == nil || !validRepo(*ref.Repo.FullName) {
			return nil, errors.New("PR commit or repository identity is unavailable or invalid")
		}
	}
	if !strings.EqualFold(*p.Base.Repo.FullName, repo.Owner+"/"+repo.Name) {
		return nil, errors.New("PR base repository does not match selected repository")
	}
	return &PullRequest{Number: *p.Number, Title: *p.Title, URL: *p.URL, State: *p.State, Draft: *p.Draft, HeadSHA: *p.Head.SHA, BaseSHA: *p.Base.SHA, HeadRef: *p.Head.Ref, BaseRef: *p.Base.Ref, HeadRepository: *p.Head.Repo.FullName, BaseRepository: *p.Base.Repo.FullName}, nil
}
func readCI(ctx context.Context, api apiReader, r *Result) error {
	prefix := fmt.Sprintf("repos/%s/%s/commits/%s/", r.Repository.Owner, r.Repository.Name, r.PullRequest.HeadSHA)
	seen := map[int64]bool{}
	total := -1
	for page := 1; page <= maxPages; page++ {
		raw, e := api.get(ctx, fmt.Sprintf("%scheck-runs?per_page=100&page=%d", prefix, page))
		if e != nil {
			return e
		}
		var response struct {
			Total *int               `json:"total_count"`
			Rows  *[]json.RawMessage `json:"check_runs"`
		}
		if !utf8.Valid(raw) || json.Unmarshal(raw, &response) != nil || response.Total == nil || response.Rows == nil || *response.Total < 0 || *response.Total > maxRecords || len(*response.Rows) > 100 {
			return errors.New("check-run collection is missing, invalid or exceeds its bounds")
		}
		if total < 0 {
			total = *response.Total
		}
		if total != *response.Total {
			return errors.New("check-run total changed during pagination")
		}
		if len(*response.Rows) == 0 && len(r.Checks) != total {
			return errors.New("check-run pagination ended before its declared total")
		}
		for _, row := range *response.Rows {
			check, e := decodeCheck(row, r.PullRequest.HeadSHA)
			if e != nil {
				return e
			}
			if seen[check.ID] {
				return errors.New("duplicate check-run identity")
			}
			seen[check.ID] = true
			r.Checks = append(r.Checks, check)
		}
		if len(r.Checks) > total {
			return errors.New("check-run count exceeded its declared total")
		}
		if len(r.Checks) == total {
			break
		}
		if page == maxPages {
			return errors.New("cannot prove check-run pagination exhaustion")
		}
	}
	seen = map[int64]bool{}
	for page := 1; page <= maxPages; page++ {
		raw, e := api.get(ctx, fmt.Sprintf("%sstatuses?per_page=100&page=%d", prefix, page))
		if e != nil {
			return e
		}
		var rows []json.RawMessage
		if !utf8.Valid(raw) || json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 100 {
			return errors.New("commit-status collection is missing or invalid")
		}
		for _, row := range rows {
			s, e := decodeStatus(row)
			if e != nil {
				return e
			}
			if seen[s.ID] {
				return errors.New("duplicate commit-status identity")
			}
			seen[s.ID] = true
			if len(r.Checks)+len(r.Statuses) >= maxRecords {
				return errors.New("combined CI records exceed the one thousand record limit")
			}
			r.Statuses = append(r.Statuses, s)
		}
		if len(rows) < 100 {
			return nil
		}
		if page == maxPages {
			return errors.New("cannot prove commit-status pagination exhaustion")
		}
	}
	return nil
}
func decodeCheck(raw []byte, sha string) (Check, error) {
	var p struct {
		ID         *int64          `json:"id"`
		Name       *string         `json:"name"`
		Status     *string         `json:"status"`
		Conclusion json.RawMessage `json:"conclusion"`
		HeadSHA    *string         `json:"head_sha"`
		URL        *string         `json:"html_url"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &p) != nil || p.ID == nil || *p.ID <= 0 || p.Name == nil || p.Status == nil || p.HeadSHA == nil || *p.HeadSHA != sha || p.URL == nil || len(p.Conclusion) == 0 {
		return Check{}, errors.New("check-run required fields or head identity are invalid")
	}
	var conclusion *string
	if json.Unmarshal(p.Conclusion, &conclusion) != nil {
		return Check{}, errors.New("invalid check-run conclusion")
	}
	return Check{*p.ID, *p.Name, *p.Status, conclusion, *p.HeadSHA, *p.URL}, nil
}
func decodeStatus(raw []byte) (CommitStatus, error) {
	var p struct {
		ID          *int64          `json:"id"`
		Context     *string         `json:"context"`
		State       *string         `json:"state"`
		Description json.RawMessage `json:"description"`
		URL         json.RawMessage `json:"target_url"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &p) != nil || p.ID == nil || *p.ID <= 0 || p.Context == nil || p.State == nil || len(p.Description) == 0 || len(p.URL) == 0 {
		return CommitStatus{}, errors.New("commit-status required fields are invalid")
	}
	var desc, url *string
	if json.Unmarshal(p.Description, &desc) != nil || json.Unmarshal(p.URL, &url) != nil {
		return CommitStatus{}, errors.New("invalid commit-status text")
	}
	s := CommitStatus{ID: *p.ID, Context: *p.Context, State: *p.State}
	if desc != nil {
		s.Description = *desc
	}
	if url != nil {
		s.URL = *url
	}
	return s, nil
}
