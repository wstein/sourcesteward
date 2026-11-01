// SPDX-License-Identifier: EUPL-1.2
package readycheck

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Source is the read side of the command: the GET reads of the watch reader plus the repository
// read. *hosting.WatchReader implements it; it has no write method.
type Source interface {
	watchcmd.Reader
	RepoInfo(ctx context.Context, repo string) (hosting.RepoInfo, error)
}

var _ Source = (*hosting.WatchReader)(nil)

// ReviewFunc returns the review state of one pull request for its current head (Spec 046). It is
// the only seam to the review-state parser: nothing else in this package knows the comment grammar.
type ReviewFunc func(ctx context.Context, repo string, number int, head string) (Review, error)

// Input names the pull request.
type Input struct {
	Repo   string
	Number int
	Host   string
}

// Document is the JSON document of `stw ready-check --json` (schema_version 1).
type Document struct {
	SchemaVersion int            `json:"schema_version"`
	Command       string         `json:"command"`
	Time          string         `json:"time"`
	Host          string         `json:"host"`
	Repo          string         `json:"repo"`
	Number        int            `json:"number"`
	Head          *string        `json:"head"`
	Draft         *bool          `json:"draft"`
	Ready         bool           `json:"ready"`
	Conditions    []DocCondition `json:"conditions"`
	Errors        []DocReadError `json:"errors"`
}

// DocCondition is one precondition: id, status (pass|fail|unknown) and the evidence behind it.
type DocCondition struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

// DocReadError is one read that failed; source is pull, repo, checks or review.
type DocReadError struct {
	Source  string `json:"source"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

const maxBody = 65536

func readError(source string, err error) DocReadError {
	code := "read-failed"
	var we *hosting.WatchError
	if errors.As(err, &we) && we.Code != "" {
		code = we.Code
	}
	return DocReadError{Source: source, Code: code, Message: snapcmd.Cut(err.Error(), snapcmd.MaxMessage)}
}

type pullDoc struct {
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	Draft          bool   `json:"draft"`
	Body           string `json:"body"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
	Rebaseable     *bool  `json:"rebaseable"`
	Head           struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func decodePull(body []byte) (pullDoc, error) {
	var p pullDoc
	if json.Unmarshal(body, &p) != nil || !validSHA(p.Head.SHA) || p.Base.Ref == "" || p.State == "" {
		return pullDoc{}, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "pull request response is not usable"}
	}
	return p, nil
}

func validSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

// Run reads pulls/N, the repository, the checks of the exact head and the review state, and
// evaluates the conditions. It issues GET reads only. A failed read makes its conditions unknown;
// without a valid head, neither the checks nor the review are read.
func Run(ctx context.Context, src Source, review ReviewFunc, in Input, now func() time.Time) Document {
	d := Document{SchemaVersion: 1, Command: "ready-check", Time: now().UTC().Format(time.RFC3339), Host: in.Host, Repo: in.Repo, Number: in.Number, Errors: []DocReadError{}}
	var f Facts
	fail := func(source string, err error) string {
		e := readError(source, err)
		d.Errors = append(d.Errors, e)
		return e.Message
	}

	res, err := src.PullDetail(ctx, in.Repo, in.Number, "")
	var p pullDoc
	switch {
	case err != nil:
		f.PullErr = fail("pull", err)
	case res.NotModified:
		f.PullErr = fail("pull", &hosting.WatchError{Code: hosting.CodeMalformed, Message: "unexpected 304 for an unconditional read"})
	default:
		if p, err = decodePull(res.Body); err != nil {
			f.PullErr = fail("pull", err)
		}
	}
	if f.PullErr == "" {
		f.State, f.Draft, f.Head, f.BaseRef = p.State, p.Draft, p.Head.SHA, p.Base.Ref
		if p.Merged {
			f.State = "merged"
		}
		f.Body, f.Mergeable, f.MergeableState, f.Rebaseable = snapcmd.Cut(p.Body, maxBody), p.Mergeable, p.MergeableState, p.Rebaseable
		head, draft := f.Head, f.Draft
		d.Head, d.Draft = &head, &draft
	}

	if info, err := src.RepoInfo(ctx, in.Repo); err != nil {
		f.RepoErr = fail("repo", err)
	} else {
		f.DefaultBranch = info.DefaultBranch
	}

	if f.PullErr == "" {
		if _, class, err := watchcmd.ReadChecks(ctx, src, in.Repo, f.Head); err != nil {
			f.ChecksErr = fail("checks", err)
		} else {
			f.ChecksClass = class
		}
		if review == nil {
			f.ReviewErr = fail("review", errors.New("no review state source"))
		} else if r, err := review(ctx, in.Repo, in.Number, f.Head); err != nil {
			f.ReviewErr = fail("review", err)
		} else {
			f.Review = r
		}
	}

	conds := Evaluate(f)
	d.Ready = Ready(conds)
	for _, c := range conds {
		d.Conditions = append(d.Conditions, DocCondition{ID: c.ID, Status: c.Status, Evidence: c.Evidence})
	}
	return d
}
