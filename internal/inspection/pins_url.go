// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// URL handling (Spec 021 FR-004, FR-009, US3-4, US4-2). A registered or remote URL is data: it is
// classified, redacted and compared as text. Nothing here contacts a URL, resolves a host or executes a
// remote helper; the only Git processes are the local configuration reads of pinsReadRemotes.

const pinsURLLimit = 4096

// pinsURLInfo is the classification of one URL text.
type pinsURLInfo struct {
	Redacted       string // credentials removed and bounded: the only form that may be shown
	HasCredentials bool   // a password, or any userinfo on a web scheme
	Scheme         string // lower-case scheme or transport name; empty for scp-like and local paths
	Kind           string // url, scp, local, relative or invalid
	Valid          bool
	Problem        string // why an invalid URL is invalid (no finding code exists for this; see tasks)
	Normalized     string // comparable form; empty when the URL cannot be compared
}

var pinsAllowedSchemes = map[string]string{ // scheme -> comparison family
	"https": "https", "http": "http", "ssh": "ssh", "git+ssh": "ssh", "ssh+git": "ssh", "git": "git", "file": "file",
}

var pinsDefaultPorts = map[string]string{"https": "443", "http": "80", "ssh": "22", "git": "9418"}

func isSchemeName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.')) {
			return false
		}
	}
	return true
}

// splitAuthority splits a scheme URL remainder into userinfo, host:port and the rest. The userinfo ends at
// the last '@' of the authority, as for Git and curl.
func splitAuthority(rest string) (userinfo string, hasUser bool, hostport, tail string) {
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	auth, tail := rest[:end], rest[end:]
	if i := strings.LastIndexByte(auth, '@'); i >= 0 {
		return auth[:i], true, auth[i+1:], tail
	}
	return "", false, auth, tail
}

func pinsClassifyURL(raw string) pinsURLInfo {
	info := pinsURLInfo{Kind: "invalid"}
	show := func(s string) string { return boundPinsMessage(s) }
	invalid := func(redacted, problem string) pinsURLInfo {
		info.Redacted, info.Problem = show(redacted), problem
		return info
	}
	// Redaction first and independent of validity: a malformed URL must not leak a credential either.
	redacted := raw
	if i := strings.Index(raw, "://"); i > 0 && isSchemeName(raw[:i]) {
		scheme := strings.ToLower(raw[:i])
		user, hasUser, hostport, tail := splitAuthority(raw[i+3:])
		if hasUser && (strings.Contains(user, ":") || scheme == "https" || scheme == "http" || scheme == "ftp" || scheme == "ftps" || pinsAllowedSchemes[scheme] == "") {
			info.HasCredentials = true
			redacted = raw[:i+3] + hostport + tail
		}
	} else if i := strings.Index(raw, "::"); i > 0 && isSchemeName(raw[:i]) {
		// transport::address is Git's remote-helper syntax; the address is opaque and may hold anything.
		redacted = raw[:i] + "::<redacted>"
		info.Scheme = strings.ToLower(raw[:i])
	}
	switch {
	case raw == "":
		return invalid("", "the URL is empty")
	case len(raw) > pinsURLLimit:
		return invalid(redacted, "the URL exceeds its length limit")
	case !utf8.ValidString(raw):
		return invalid(redacted, "the URL is not valid UTF-8")
	case strings.IndexFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
		return invalid(redacted, "the URL contains a control character")
	case strings.HasPrefix(raw, "-"):
		// Never a URL and unparseable: nothing of it is shown, so it cannot carry a credential.
		return invalid("<redacted>", "the URL begins with a dash and could be read as an option")
	case strings.HasPrefix(raw, "://"):
		return invalid(redacted, "the URL has an empty scheme")
	case info.Scheme != "":
		return invalid(redacted, "the URL uses Git's remote-helper syntax, which is never run")
	}
	info.Redacted = show(redacted)
	if i := strings.Index(raw, "://"); i > 0 && isSchemeName(raw[:i]) {
		scheme := strings.ToLower(raw[:i])
		info.Scheme = scheme
		family, ok := pinsAllowedSchemes[scheme]
		if !ok {
			info.Problem = "the URL scheme is not supported"
			return info
		}
		_, _, hostport, tail := splitAuthority(raw[i+3:])
		if hostport == "" && family != "file" {
			info.Problem = "the URL has no host"
			return info
		}
		info.Kind, info.Valid = "url", true
		path := tail
		if q := strings.IndexAny(path, "?#"); q >= 0 {
			path = path[:q]
		}
		host, port := strings.ToLower(hostport), ""
		if j := strings.LastIndexByte(host, ':'); j >= 0 && !strings.HasSuffix(host, "]") {
			host, port = host[:j], host[j+1:]
		}
		if port == pinsDefaultPorts[family] {
			port = ""
		}
		if family == "file" {
			info.Normalized = "file://" + pinsNormalizePath(path)
			return info
		}
		if port != "" {
			host += ":" + port
		}
		info.Normalized = family + "://" + host + pinsNormalizePath(path)
		return info
	}
	colon, slash := strings.IndexByte(raw, ':'), strings.IndexByte(raw, '/')
	switch {
	case colon > 0 && (slash < 0 || colon < slash):
		// scp-like: [user@]host:path. A password cannot be written in this form, so none is reported.
		host := raw[:colon]
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
		info.Kind, info.Valid = "scp", true
		info.Normalized = "ssh://" + strings.ToLower(host) + pinsNormalizePath("/"+strings.TrimPrefix(raw[colon+1:], "/"))
	case strings.HasPrefix(raw, "/"):
		info.Kind, info.Valid = "local", true
		info.Normalized = "file://" + pinsNormalizePath(raw)
	default:
		info.Kind, info.Valid = "relative", true // resolved against the superproject remote: not comparable
	}
	return info
}

// pinsNormalizePath drops trailing slashes and a trailing .git so spellings of one repository compare equal.
func pinsNormalizePath(p string) string {
	for {
		q := strings.TrimSuffix(strings.TrimRight(p, "/"), ".git")
		if q == p {
			return p
		}
		p = q
	}
}

// pinsRemote is one remote.<name>.url of a submodule's own configuration.
type pinsRemote struct{ Name, URL string }

// pinsReadRemotes reads the remote URLs from the validated submodule's configuration. Git only reads the
// configuration; no URL is contacted.
func pinsReadRemotes(run *pinsRunner, repo pinsGitRepo) ([]pinsRemote, error) {
	out, err := run.run(pinsCall{Repo: repo, Args: []string{"config", "--local", "--null", "--list", "--no-includes"}, Keep: func(rec []byte) bool {
		k, _, _ := bytes.Cut(rec, []byte("\n"))
		return bytes.HasPrefix(k, []byte("remote.")) && bytes.HasSuffix(k, []byte(".url"))
	}})
	if err != nil {
		return nil, err
	}
	if out.Exit != 0 {
		return nil, fmt.Errorf("cannot read the submodule remotes")
	}
	var remotes []pinsRemote
	for _, kv := range parsePinsConfig(out.Stdout) {
		name := strings.TrimSuffix(strings.TrimPrefix(kv.Key, "remote."), ".url")
		if name != "" && kv.HasValue {
			remotes = append(remotes, pinsRemote{Name: name, URL: kv.Value})
		}
	}
	return remotes, nil
}

// pinsURLMatch compares a registered URL with the submodule's own remotes. URLs are redacted.
type pinsURLMatch struct {
	Match      string // match, mismatch or unknown
	RemoteName *string
	RemoteURL  *string
}

// pinsMatchRemote finds "the remote the submodule uses": the one remote whose normalized URL equals the
// registered URL. Several equal remotes make the answer unknown. When no remote is equal, a submodule with
// exactly one comparable remote has a mismatch (both URLs are known); with none or several the answer is
// unknown, because no remote can be said to be the one in use.
func pinsMatchRemote(registered pinsURLInfo, remotes []pinsRemote) pinsURLMatch {
	unknown := pinsURLMatch{Match: "unknown"}
	if !registered.Valid || registered.Normalized == "" {
		return unknown
	}
	names := map[string]pinsURLInfo{}
	var matched []string
	for _, r := range remotes {
		info := pinsClassifyURL(r.URL)
		if _, seen := names[r.Name]; !seen {
			names[r.Name] = info
		}
		if info.Normalized == registered.Normalized {
			names[r.Name] = info
			if !slices.Contains(matched, r.Name) {
				matched = append(matched, r.Name)
			}
		}
	}
	report := func(match, name string) pinsURLMatch {
		url := names[name].Redacted
		return pinsURLMatch{Match: match, RemoteName: &name, RemoteURL: &url}
	}
	switch {
	case len(matched) == 1:
		return report("match", matched[0])
	case len(matched) > 1:
		return unknown
	case len(names) == 1:
		for name, info := range names {
			if info.Valid && info.Normalized != "" {
				return report("mismatch", name)
			}
		}
	}
	return unknown
}
