// SPDX-License-Identifier: EUPL-1.2
package lintprcmd

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/wstein/sourcesteward/internal/lintpr"
)

var errShape = errors.New("malformed-response: unexpected document shape")

var sha40 = regexp.MustCompile(`\A[0-9a-f]{40}\z`)

// decodeDetail reads title, body and head SHA of pulls/N. A null body is the empty text.
func decodeDetail(raw []byte) (title, body, head string, err error) {
	var d struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
		Head  struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if json.Unmarshal(raw, &d) != nil || !sha40.MatchString(d.Head.SHA) {
		return "", "", "", errShape
	}
	if d.Title != nil {
		title = *d.Title
	}
	if d.Body != nil {
		body = *d.Body
	}
	return title, body, d.Head.SHA, nil
}

type person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// decodeCommits reads one page of pulls/N/commits.
func decodeCommits(raw []byte) ([]lintpr.Commit, error) {
	var items []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message   string  `json:"message"`
			Author    *person `json:"author"`
			Committer *person `json:"committer"`
		} `json:"commit"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return nil, errShape
	}
	out := make([]lintpr.Commit, 0, len(items))
	for _, it := range items {
		if !sha40.MatchString(it.SHA) {
			return nil, errShape
		}
		c := lintpr.Commit{SHA: it.SHA, Message: it.Commit.Message}
		if a := it.Commit.Author; a != nil {
			c.AuthorName, c.AuthorEmail = a.Name, a.Email
		}
		if a := it.Commit.Committer; a != nil {
			c.CommitterName, c.CommitterEmail = a.Name, a.Email
		}
		out = append(out, c)
	}
	return out, nil
}

// decodeComments reads one page of issues/N/comments. The ID is required; a null body is empty.
func decodeComments(raw []byte) ([]lintpr.Comment, error) {
	var items []struct {
		ID   int64   `json:"id"`
		Body *string `json:"body"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return nil, errShape
	}
	out := make([]lintpr.Comment, 0, len(items))
	for _, it := range items {
		if it.ID < 1 {
			return nil, errShape
		}
		c := lintpr.Comment{ID: it.ID}
		if it.Body != nil {
			c.Body = *it.Body
		}
		out = append(out, c)
	}
	return out, nil
}
