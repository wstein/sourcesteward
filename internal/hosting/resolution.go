// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

func parseRemote(remote, raw, host string) (*Candidate, error) {
	if raw == "" || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return nil, errors.New("malformed remote URL")
	}
	if !strings.Contains(strings.ToLower(raw), strings.ToLower(host)) {
		return nil, nil
	}
	var h, p string
	if strings.HasPrefix(raw, "git@") && !strings.Contains(raw, "://") {
		after := strings.TrimPrefix(raw, "git@")
		var ok bool
		h, p, ok = strings.Cut(after, ":")
		if !ok {
			return nil, errors.New("malformed remote URL")
		}
	} else {
		u, e := url.Parse(raw)
		if e != nil {
			return nil, errors.New("malformed remote URL")
		}
		h = u.Hostname()
		if h != "" && !strings.EqualFold(h, host) {
			return nil, nil
		}
		if u.Scheme != "https" && u.Scheme != "ssh" {
			return nil, errors.New("unsupported selected-host remote URL")
		}
		if u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Fragment != "" || u.RawPath != "" || u.Port() != "" || u.Opaque != "" {
			return nil, errors.New("malformed selected-host remote URL")
		}
		if u.User != nil && (u.Scheme != "ssh" || u.User.Username() != "git" || u.User.String() != "git") {
			return nil, errors.New("credentialed remote URL refused")
		}
		p = strings.TrimPrefix(u.Path, "/")
	}
	if !strings.EqualFold(h, host) {
		return nil, nil
	}
	p = strings.TrimSuffix(p, ".git")
	if !validRepo(p) {
		return nil, errors.New("malformed selected-host repository path")
	}
	parts := strings.Split(p, "/")
	return &Candidate{Remote: remote, Host: strings.ToLower(host), Owner: parts[0], Name: parts[1]}, nil
}
func selectRepository(o Options, cs []Candidate) (*Repository, error) {
	host := o.Host
	if host == "" {
		host = "github.com"
	}
	host = strings.ToLower(host)
	filtered := cs
	if o.Remote != "" {
		filtered = nil
		for _, c := range cs {
			if c.Remote == o.Remote {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return nil, errors.New("selected remote has no valid repository for the selected host")
		}
	}
	unique := map[string]Candidate{}
	for _, c := range filtered {
		unique[strings.ToLower(c.Host+"/"+c.Owner+"/"+c.Name)] = c
	}
	if o.Repo != "" {
		p := strings.Split(o.Repo, "/")
		r := &Repository{host, p[0], p[1], "explicit-repository"}
		if o.Remote != "" {
			if len(unique) != 1 {
				return nil, errors.New("selected remote has ambiguous fetch/push repository identities")
			}
			for _, c := range unique {
				if !strings.EqualFold(c.Owner+"/"+c.Name, o.Repo) {
					return nil, errors.New("repository and remote selections disagree")
				}
			}
		}
		return r, nil
	}
	if len(unique) == 0 {
		return nil, errors.New("no repository candidate; select --repo OWNER/NAME")
	}
	if len(unique) > 1 {
		return nil, errors.New("ambiguous repository candidates; select --repo or --remote")
	}
	for _, c := range unique {
		selection := "unique-candidate"
		if o.Remote != "" {
			selection = "explicit-remote"
		}
		return &Repository{host, c.Owner, c.Name, selection}, nil
	}
	panic("unreachable")
}
