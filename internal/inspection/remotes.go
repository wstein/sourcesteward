// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Remote preserves every sampled native fetch and push URL. URLs are untrusted
// local configuration and must not select an authentication host implicitly.
type Remote struct {
	Name      string
	FetchURLs []string
	PushURLs  []string
}

// RemoteResult is an internal operation result, separate from published schemas.
type RemoteResult struct {
	Identity    Result
	Status      string
	Remotes     []Remote
	Diagnostics []Diagnostic
}

// Remotes samples native active remotes twice without fetching or snapshotting.
// The expected identity and configuration are sampled, not transactionally locked.
func Remotes(ctx context.Context, identity Result) RemoteResult {
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	return remotes(ctx, identity, Inspect, readRemotes)
}

func remotes(ctx context.Context, identity Result, inspect func(context.Context, string) Result, read func(context.Context, Result) ([]Remote, error)) RemoteResult {
	r := RemoteResult{Identity: identity, Status: "error", Remotes: []Remote{}, Diagnostics: []Diagnostic{}}
	fail := func(code, message string) RemoteResult {
		r.Remotes = []Remote{}
		r.Diagnostics = []Diagnostic{{code, message}}
		return r
	}
	if ctx.Err() != nil {
		return fail("canceled", "remote inspection canceled")
	}
	if identity.Status != "ok" || identity.InputPath == "" || identity.Backend != "git" && identity.Backend != "jj" || identity.Backend == "git" && identity.Repository == nil || identity.Backend == "jj" && (identity.JJRepository == nil || len(identity.Evidence) == 0) {
		return fail("invalid-identity", "valid native ownership identity is required")
	}
	first := inspect(ctx, identity.InputPath)
	if first.Status != "ok" || !reflect.DeepEqual(identity, first) {
		return fail("changed-repository", "repository identity changed before remote inspection")
	}
	a, err := read(ctx, identity)
	if err != nil {
		return fail("remote-inspection-failed", "cannot safely enumerate native remotes")
	}
	b, err := read(ctx, identity)
	if ctx.Err() != nil {
		return fail("canceled", "remote inspection canceled or exceeded its deadline")
	}
	if err != nil {
		return fail("remote-inspection-failed", "cannot safely resample native remotes")
	}
	if !reflect.DeepEqual(a, b) {
		return fail("changed-repository", "remote configuration changed during inspection")
	}
	final := inspect(ctx, identity.InputPath)
	if ctx.Err() != nil {
		return fail("canceled", "remote inspection canceled or exceeded its deadline")
	}
	if final.Status != "ok" || !reflect.DeepEqual(identity, final) {
		return fail("changed-repository", "repository identity changed during remote inspection")
	}
	r.Status = "ok"
	r.Remotes = a
	return r
}

func readRemotes(ctx context.Context, id Result) ([]Remote, error) {
	if id.Backend == "jj" {
		var remotes []Remote
		err := withJJSession(ctx, id.InputPath, id, func(s jjSession, _, _, _ string) error {
			raw, err := s.read(id.InputPath, id.JJRepository.OperationID, "git", "remote", "list")
			if err != nil {
				return err
			}
			remotes, err = parseJJRemotes(raw)
			return err
		})
		return remotes, err
	}
	return readGitRemotes(ctx, id, runGit)
}

func remoteAtom(value string) bool {
	return value != "" && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n") && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func remoteLines(raw string) ([]string, error) {
	if len(raw) > outputLimit || !utf8.ValidString(raw) {
		return nil, errors.New("invalid remote output")
	}
	if raw == "" {
		return []string{}, nil
	}
	if !strings.HasSuffix(raw, "\n") {
		return nil, errors.New("unterminated remote output")
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) > inventoryRecordLimit {
		return nil, errors.New("remote count exceeded")
	}
	return lines, nil
}

func parseJJRemotes(raw string) ([]Remote, error) {
	lines, err := remoteLines(raw)
	if err != nil {
		return nil, err
	}
	result := []Remote{}
	seen := map[string]bool{}
	for _, line := range lines {
		name, tail, ok := strings.Cut(line, " ")
		if !ok || !remoteAtom(name) || seen[name] {
			return nil, errors.New("invalid remote record")
		}
		seen[name] = true
		fetch, push := tail, tail
		if i := strings.Index(tail, " (push: "); i >= 0 {
			if !strings.HasSuffix(tail, ")") {
				return nil, errors.New("invalid remote push record")
			}
			fetch = tail[:i]
			push = tail[i+8 : len(tail)-1]
		}
		if !remoteAtom(fetch) || !remoteAtom(push) {
			return nil, errors.New("unsupported remote URL")
		}
		result = append(result, Remote{Name: name, FetchURLs: []string{fetch}, PushURLs: []string{push}})
	}
	return result, nil
}

func readGitRemotes(ctx context.Context, id Result, run runner) ([]Remote, error) {
	raw, code, err := run(ctx, id.InputPath, "remote")
	if err != nil || code != 0 {
		return nil, errors.New("cannot enumerate Git remotes")
	}
	names, err := remoteLines(raw)
	if err != nil {
		return nil, err
	}
	result := []Remote{}
	seen := map[string]bool{}
	count := 0
	bytes := len(raw)
	for _, name := range names {
		if !remoteAtom(name) || strings.HasPrefix(name, "-") || seen[name] {
			return nil, errors.New("invalid remote name")
		}
		seen[name] = true
		item := Remote{Name: name}
		for _, push := range []bool{false, true} {
			args := []string{"remote", "get-url", "--all"}
			if push {
				args = append(args, "--push")
			}
			args = append(args, name)
			raw, code, err = run(ctx, id.InputPath, args...)
			if err != nil || code != 0 {
				return nil, errors.New("cannot read Git remote URLs")
			}
			urls, err := remoteLines(raw)
			if err != nil || len(urls) == 0 {
				return nil, errors.New("invalid Git remote URLs")
			}
			count += len(urls)
			bytes += len(raw)
			if count > inventoryRecordLimit || bytes > outputLimit {
				return nil, errors.New("remote output exceeded bound")
			}
			for _, url := range urls {
				if !remoteAtom(url) {
					return nil, errors.New("unsupported Git remote URL")
				}
			}
			if push {
				item.PushURLs = urls
			} else {
				item.FetchURLs = urls
			}
		}
		result = append(result, item)
	}
	return result, nil
}
