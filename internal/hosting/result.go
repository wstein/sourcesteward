// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"fmt"
	"github.com/wstein/sourcesteward/internal/inspection"
	"regexp"
	"strings"
	"unicode"
)

type Options struct {
	Kind               string
	Number             int
	Status             bool   // explicit schema 2 one-shot PR observation
	ExpectHead         string // full 40-hex head SHA the PR must have (Status only)
	HeadSHA            string // full 40-hex SHA to look up (kind prs)
	Repo, Host, Remote string
	RequireJJ          bool
}
type Repository struct {
	Host      string `json:"host"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Selection string `json:"selection"`
}
type Candidate struct {
	Remote string `json:"remote"`
	Host   string `json:"host"`
	Owner  string `json:"owner"`
	Name   string `json:"name"`
}
type PullRequest struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	HeadSHA        string `json:"head_sha"`
	BaseSHA        string `json:"base_sha"`
	HeadRef        string `json:"head_ref"`
	BaseRef        string `json:"base_ref"`
	HeadRepository string `json:"head_repository"`
	BaseRepository string `json:"base_repository"`
}
type Check struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	HeadSHA    string  `json:"head_sha"`
	URL        string  `json:"url"`
}
type CommitStatus struct {
	ID          int64  `json:"id"`
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
	URL         string `json:"url"`
}
type Boundary struct {
	Complete       bool     `json:"complete"`
	Network        bool     `json:"network"`
	MaxPages       int      `json:"max_pages"`
	MaxRecords     int      `json:"max_records"`
	MaxBytes       int      `json:"max_bytes"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Limitations    []string `json:"limitations"`
}
type Result struct {
	SchemaVersion int                     `json:"schema_version"`
	Kind          string                  `json:"kind"`
	Status        string                  `json:"status"`
	Backend       string                  `json:"backend"`
	Identity      *inspection.Result      `json:"identity"`
	Repository    *Repository             `json:"repository"`
	Candidates    []Candidate             `json:"candidates"`
	PullRequest   *PullRequest            `json:"pull_request"`
	Checks        []Check                 `json:"checks"`
	Statuses      []CommitStatus          `json:"statuses"`
	Diagnostics   []inspection.Diagnostic `json:"diagnostics"`
	Boundary      Boundary                `json:"boundary"`
	ObservedAt    string                  `json:"observed_at"`
}

func (r Result) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	return "hosting inspection failed"
}

var segment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
var label = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`)

func validRepo(s string) bool {
	p := strings.Split(s, "/")
	return len(p) == 2 && segment.MatchString(p[0]) && segment.MatchString(p[1]) && len(p[0]) <= 100 && len(p[1]) <= 100 && p[0] != "." && p[1] != "."
}
func validHost(s string) bool {
	if len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, p := range strings.Split(s, ".") {
		if len(p) > 63 || !label.MatchString(p) {
			return false
		}
	}
	return true
}
func ValidateOptions(o Options) error {
	if o.Kind != "repo" && o.Kind != "pr" && o.Kind != "ci" && o.Kind != "prs" {
		return fmt.Errorf("kind must be repo, pr, prs or ci")
	}
	if o.Kind == "prs" {
		if o.Number != 0 || o.Status || o.ExpectHead != "" || !fullSHA(o.HeadSHA) {
			return fmt.Errorf("prs requires exactly one full 40-character lowercase hexadecimal head SHA")
		}
	} else if o.HeadSHA != "" {
		return fmt.Errorf("a head SHA lookup requires the prs kind")
	} else if (o.Kind == "repo" && o.Number != 0) || (o.Kind != "repo" && o.Number <= 0) {
		return fmt.Errorf("PR and CI require a positive PR number")
	}
	if o.Status && o.Kind != "pr" {
		return fmt.Errorf("schema 2 status applies only to PR observations")
	}
	if o.ExpectHead != "" && (!o.Status || !fullSHA(o.ExpectHead)) {
		return fmt.Errorf("expected head must be a full 40-character lowercase hexadecimal SHA and requires schema 2")
	}
	if o.Repo != "" && !validRepo(o.Repo) {
		return fmt.Errorf("repository must be OWNER/NAME")
	}
	if o.Host != "" && !validHost(o.Host) {
		return fmt.Errorf("host must be a DNS hostname")
	}
	if len(o.Remote) > 1024 || strings.IndexFunc(o.Remote, unicode.IsControl) >= 0 {
		return fmt.Errorf("remote name contains invalid characters")
	}
	return nil
}
