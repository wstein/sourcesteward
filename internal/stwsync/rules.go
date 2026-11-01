// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The agent-rules checkout (wstein/agent-rules, the rules every agent session loads) is reported by every
// `stw sync` that finds or is pointed at one, and updated only by `stw sync --rules`, only by fast-forward and only
// when every new commit is by an owner identity (Spec 032 amendment, issue 363). stw never runs install.sh and
// never writes to ~/.claude or to a Git configuration.

// State of the agent-rules checkout.
const (
	RulesCurrent    = "current"
	RulesBehind     = "behind"
	RulesAhead      = "ahead"
	RulesDiverged   = "diverged"
	RulesDirty      = "dirty"
	RulesNotDefault = "not-on-default-branch"
	RulesNotFound   = "not-found"
)

// Outcome of --rules; none when only the state was asked for.
const (
	OutcomeNone             = "none"
	OutcomeUpToDate         = "up-to-date"
	OutcomeWouldFastForward = "would-fast-forward"
	OutcomeFastForwarded    = "fast-forwarded"
	OutcomeRefused          = "refused"
	OutcomeFailed           = "failed"
)

const (
	rulesCommitLimit  = 10000 // more new commits than this are refused: all of them must be checked
	rulesCommitsShown = 100
	rulesFilesShown   = 500
	rulesRepoName     = "agent-rules"
)

// RulesCommit is one new commit of origin/DEFAULT, as it will be after --rules.
type RulesCommit struct {
	OID       string `json:"oid"`
	Subject   string `json:"subject"`
	Author    string `json:"author"`
	Committer string `json:"committer"`
}

// RulesFile is one file the update changes; Sensitive marks what shapes agent behavior or installs it.
type RulesFile struct {
	Path      string `json:"path"`
	Sensitive bool   `json:"sensitive"`
}

// Rules is the report on the agent-rules checkout. Source is workspace, flag or env. Install is symlink (the
// user's ~/.claude/CLAUDE.md points into the checkout), copy (it is a file or points elsewhere) or absent.
type Rules struct {
	State         string        `json:"state"`
	Source        string        `json:"source"`
	Path          *string       `json:"path"`
	Branch        *string       `json:"branch"`
	DefaultBranch *string       `json:"default_branch"`
	Ahead         int           `json:"ahead"`
	Behind        int           `json:"behind"`
	AsOfLastFetch bool          `json:"as_of_last_fetch"`
	Install       string        `json:"install"`
	Outcome       string        `json:"outcome"`
	OldOID        *string       `json:"old_oid"`
	NewOID        *string       `json:"new_oid"`
	Reason        *string       `json:"reason"`
	Commits       []RulesCommit `json:"commits"`
	Files         []RulesFile   `json:"files"`
	Notes         []string      `json:"notes"`
}

type identity struct{ name, email string }

var identityPattern = regexp.MustCompile(`^([^<>\x00-\x1f\x7f]*[^<>\s\x00-\x1f\x7f])\s*<([^<>\s\x00-\x1f\x7f]+@[^<>\s\x00-\x1f\x7f]+)>$`)

func parseIdentity(raw string) (identity, error) {
	m := identityPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil || !utf8.ValidString(raw) {
		return identity{}, fmt.Errorf("%q is not \"Name <email>\"", oneLine(raw))
	}
	return identity{m[1], strings.ToLower(m[2])}, nil
}

func (i identity) String() string { return i.name + " <" + i.email + ">" }

// matches compares the name exactly and the e-mail address without case.
func (i identity) matches(name, email string) bool {
	return i.name == name && i.email == strings.ToLower(email)
}

func anyIdentity(list []identity, name, email string) bool {
	for _, i := range list {
		if i.matches(name, email) {
			return true
		}
	}
	return false
}

// WithRulesEnvironment completes the options from the environment: STW_RULES_PATH when no --rules-path was given
// (source env), and the user's home directory, which is only read to look at ~/.claude/CLAUDE.md.
func WithRulesEnvironment(o Options) Options {
	if o.RulesPath == "" {
		o.RulesPath, o.RulesPathSource = os.Getenv("STW_RULES_PATH"), "env"
	}
	if o.Home == "" {
		if home, err := os.UserHomeDir(); err == nil {
			o.Home = home
		}
	}
	return o
}

// validateRulesOptions is the usage check of the rules options; it runs inside ValidateOptions.
func validateRulesOptions(o Options) error {
	if o.RulesPath != "" {
		p := o.RulesPath
		switch {
		case len(p) > 4096 || !utf8.ValidString(p) || strings.ContainsRune(p, 0):
			return errors.New("--rules-path must be valid UTF-8 text without NUL, at most 4096 bytes")
		case strings.HasPrefix(p, "-"):
			return errors.New("--rules-path must not start with \"-\"; prefix it with ./")
		}
		for _, part := range strings.Split(filepath.ToSlash(p), "/") {
			if part == ".." {
				return errors.New("--rules-path must not contain \"..\"")
			}
		}
		if o.RulesPathSource != "" && o.RulesPathSource != "flag" && o.RulesPathSource != "env" {
			return errors.New("the source of the rules path must be flag or env")
		}
	}
	for _, raw := range o.OwnerIdentities {
		if _, err := parseIdentity(raw); err != nil {
			return fmt.Errorf("--owner-identity %w", err)
		}
	}
	for _, raw := range o.AllowedCommitters {
		if _, err := parseIdentity(raw); err != nil {
			return fmt.Errorf("--allowed-committer %w", err)
		}
	}
	return nil
}

// rules reports on the agent-rules checkout and, with Options.Rules, fast-forwards it. It returns nil when
// nothing asked for it and none was found among the workspace members.
func (s *session) rules(ctx context.Context, root env, repos []Repo) *Rules {
	o := s.opts
	r := &Rules{State: RulesNotFound, Source: "none", Install: "absent", Outcome: OutcomeNone, Commits: []RulesCommit{}, Files: []RulesFile{}, Notes: []string{}}
	path, source, problem := locateRules(ctx, root, o, repos)
	if path == "" && source == "none" && !o.Rules && o.RulesPath == "" {
		return nil
	}
	r.Source = source
	refuse := func(reason string) *Rules {
		r.Outcome, r.Reason = OutcomeRefused, ptr(oneLine(reason))
		return r
	}
	if path == "" {
		r.Reason = ptr(oneLine(problem))
		if o.Rules {
			r.Outcome = OutcomeRefused
		}
		return r
	}
	r.Path = ptr(path)
	e := root.withDir(path)
	r.Install = installKind(o.Home, path)
	if reason, _ := e.operation(ctx, path); reason != "" {
		r.State, r.Reason = RulesDirty, ptr(oneLine(reason))
		if o.Rules {
			return refuse(reason)
		}
		return r
	}
	def, problem := rulesDefaultBranch(ctx, e)
	if def == "" {
		r.Reason = ptr(problem)
		if o.Rules {
			return refuse(problem)
		}
		return r
	}
	r.DefaultBranch = ptr(def)
	upRef := "refs/remotes/origin/" + def
	r.AsOfLastFetch = !rulesFetch(ctx, e)
	if r.AsOfLastFetch {
		r.Notes = append(r.Notes, "the fetch of origin did not run or failed; state as of last fetch")
	}
	upOID := e.revParse(ctx, path, upRef+"^{commit}")
	headOID := e.revParse(ctx, path, "HEAD^{commit}")
	if upOID == "" || headOID == "" || !oidPattern.MatchString(upOID) || !oidPattern.MatchString(headOID) {
		r.Reason = ptr("cannot resolve HEAD and origin/" + def)
		if o.Rules {
			return refuse(*r.Reason)
		}
		return r
	}
	r.OldOID = ptr(headOID)
	if sym, _ := e.g(ctx, "symbolic-ref", "-q", "HEAD"); strings.HasPrefix(strings.TrimSpace(sym.Out), "refs/heads/") {
		r.Branch = ptr(strings.TrimPrefix(strings.TrimSpace(sym.Out), "refs/heads/"))
	}
	counts, err := e.g(ctx, "rev-list", "--left-right", "--count", headOID+"..."+upOID)
	f := strings.Fields(counts.Out)
	if err != nil || counts.Code != 0 || len(f) != 2 {
		r.Reason = ptr("HEAD cannot be compared with origin/" + def)
		if o.Rules {
			return refuse(*r.Reason)
		}
		return r
	}
	r.Ahead, _ = strconv.Atoi(f[0])
	r.Behind, _ = strconv.Atoi(f[1])
	dirty := hasTrackedChanges(ctx, e, path)
	switch {
	case r.Branch == nil || *r.Branch != def:
		r.State = RulesNotDefault
	case dirty:
		r.State = RulesDirty
	case r.Ahead > 0 && r.Behind > 0:
		r.State = RulesDiverged
	case r.Behind > 0:
		r.State = RulesBehind
	case r.Ahead > 0:
		r.State = RulesAhead
	default:
		r.State = RulesCurrent
	}
	if !o.Rules {
		return r
	}
	return s.updateRules(ctx, root, e, r, def, upOID, headOID, dirty, refuse)
}

// updateRules checks every precondition of --rules in a fixed order and changes the checkout only after all of
// them hold: clean, default branch, fast-forward, owner identities on every new commit, then `merge --ff-only`.
func (s *session) updateRules(ctx context.Context, root, e env, r *Rules, def, upOID, headOID string, dirty bool, refuse func(string) *Rules) *Rules {
	switch r.State {
	case RulesCurrent, RulesAhead:
		r.Outcome = OutcomeUpToDate
		return r
	case RulesNotDefault:
		return refuse("not on the default branch " + def + "; switch to it yourself first")
	}
	if dirty {
		return refuse("the worktree has uncommitted changes")
	}
	if out, err := e.g(ctx, "merge-base", "--is-ancestor", headOID, upOID); err != nil || out.Code != 0 {
		return refuse(fmt.Sprintf("diverged from origin/%s (%d ahead, %d behind); not a fast-forward", def, r.Ahead, r.Behind))
	}
	if r.Behind > rulesCommitLimit {
		return refuse(fmt.Sprintf("more than %d new commits cannot be checked", rulesCommitLimit))
	}
	files, err := rulesChangedFiles(ctx, e, headOID, upOID)
	if err != nil {
		return refuse(err.Error())
	}
	r.Files = files
	if collide := untrackedCollisions(ctx, e, headOID, upOID); len(collide) > 0 {
		return refuse("untracked files would be overwritten: " + strings.Join(collide, ", "))
	}
	commits, bad, err := rulesCommits(ctx, e, headOID, upOID, s.ownerIdentities(ctx, root), s.committerIdentities(ctx, root))
	if err != nil {
		return refuse(err.Error())
	}
	r.Commits = commits
	if len(bad) > 0 {
		more := ""
		if len(bad) > 3 {
			more = fmt.Sprintf(" (and %d more)", len(bad)-3)
			bad = bad[:3]
		}
		return refuse("not by an owner identity: " + strings.Join(bad, "; ") + more)
	}
	if s.opts.Plan {
		r.Outcome, r.NewOID = OutcomeWouldFastForward, ptr(upOID)
		return r
	}
	// the checks ran on this exact commit; merge exactly it, never a ref that could have moved
	out, err := e.g(ctx, "-c", "submodule.recurse=false", "merge", "--ff-only", "--no-autostash", upOID)
	newOID := e.revParse(ctx, e.dir, "HEAD")
	if err != nil || out.Code != 0 || newOID != upOID {
		r.Outcome, r.Reason = OutcomeFailed, ptr(oneLine("git merge --ff-only failed: "+firstLine(out.Diagnostic+"\n"+out.Out)))
		return r
	}
	r.Outcome, r.NewOID, r.State = OutcomeFastForwarded, ptr(newOID), RulesCurrent
	r.Ahead, r.Behind = 0, 0
	r.Notes = append(r.Notes, "applies to NEW sessions; running agents keep their loaded rules")
	if r.Install == "copy" {
		r.Notes = append(r.Notes, "~/.claude/CLAUDE.md is not a symlink into this checkout: run install.sh --dry-run first (owner action)")
	}
	return r
}

// ownerIdentities are --owner-identity, else user.name and user.email of the Git configuration of the workspace root.
func (s *session) ownerIdentities(ctx context.Context, root env) []identity {
	var ids []identity
	for _, raw := range s.opts.OwnerIdentities {
		if i, err := parseIdentity(raw); err == nil {
			ids = append(ids, i)
		}
	}
	if len(ids) == 0 {
		name, email := root.config(ctx, "user.name"), root.config(ctx, "user.email")
		if name != "" && email != "" {
			ids = append(ids, identity{name, strings.ToLower(email)})
		}
	}
	return ids
}

// committerIdentities are the owner identities and --allowed-committer (the GitHub rebase-merge committer).
func (s *session) committerIdentities(ctx context.Context, root env) []identity {
	ids := s.ownerIdentities(ctx, root)
	for _, raw := range s.opts.AllowedCommitters {
		if i, err := parseIdentity(raw); err == nil {
			ids = append(ids, i)
		}
	}
	return ids
}

// locateRules finds the checkout: the explicit path (flag or environment), else the one workspace member whose
// remote is named agent-rules. Nothing else is searched.
func locateRules(ctx context.Context, root env, o Options, repos []Repo) (path, source, problem string) {
	if o.RulesPath != "" {
		source = o.RulesPathSource
		if source == "" {
			source = "flag"
		}
		abs, err := filepath.Abs(o.RulesPath)
		if err != nil {
			return "", source, "the rules path cannot be resolved"
		}
		top, err := root.in(ctx, abs, "rev-parse", "--show-toplevel")
		if info, serr := os.Stat(abs); serr != nil || !info.IsDir() || err != nil || top.Code != 0 || !samePath(strings.TrimSpace(top.Out), abs) {
			return "", source, "the rules path " + oneLine(o.RulesPath) + " is not the top of a Git checkout"
		}
		return abs, source, ""
	}
	var found []string
	for _, rp := range repos {
		if rp.Result == ResultSkippedUninitialized {
			continue
		}
		if repoNameOf(remoteURL(ctx, root.withDir(rp.Path))) == rulesRepoName {
			found = append(found, rp.Path)
		}
	}
	switch len(found) {
	case 0:
		return "", "none", "no workspace member has a remote named " + rulesRepoName + "; pass --rules-path PATH or set STW_RULES_PATH"
	case 1:
		return found[0], "workspace", ""
	}
	return "", "workspace", "several workspace members have a remote named " + rulesRepoName + "; pass --rules-path PATH"
}

// repoNameOf is the last path element of a remote URL without ".git" and trailing slashes.
func repoNameOf(url string) string {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	url = strings.TrimSuffix(url, ".git")
	if i := strings.LastIndexAny(url, "/:\\"); i >= 0 {
		url = url[i+1:]
	}
	return strings.ToLower(url)
}

// rulesDefaultBranch is the branch origin/HEAD names, else main or master when only one of them exists remotely.
func rulesDefaultBranch(ctx context.Context, e env) (branch, problem string) {
	if out, err := e.g(ctx, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil && out.Code == 0 {
		if b := strings.TrimPrefix(strings.TrimSpace(out.Out), "refs/remotes/origin/"); b != "" && b != strings.TrimSpace(out.Out) && validBranchName(ctx, e, b) {
			return b, ""
		}
	}
	for _, b := range []string{"main", "master"} {
		if e.revParse(ctx, e.dir, "refs/remotes/origin/"+b) != "" {
			return b, ""
		}
	}
	return "", "the default branch of origin cannot be determined (no origin/HEAD, no origin/main or origin/master)"
}

func validBranchName(ctx context.Context, e env, b string) bool {
	if strings.HasPrefix(b, "-") {
		return false
	}
	out, err := e.g(ctx, "check-ref-format", "--branch", b)
	return err == nil && out.Code == 0
}

// rulesFetch fetches origin the way stw sync fetches a repository, after the check that the configured refspecs
// write only below refs/remotes/origin/. It reports whether the fetch ran.
func rulesFetch(ctx context.Context, e env) bool {
	const prefix = "refs/remotes/origin/"
	if checkRefspecs(ctx, e, "origin", prefix) != nil {
		return false
	}
	out, err := e.g(ctx, "fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", "origin")
	return err == nil && out.Code == 0
}

// rulesChangedFiles lists the files that differ between the two commits, sensitive ones first.
func rulesChangedFiles(ctx context.Context, e env, from, to string) ([]RulesFile, error) {
	out, err := e.g(ctx, "diff", "--name-only", "-z", "--no-renames", from, to)
	if err != nil || out.Code != 0 {
		return nil, errors.New("the changed files cannot be listed")
	}
	var sensitive, plain []RulesFile
	for _, p := range strings.Split(out.Out, "\x00") {
		if p == "" {
			continue
		}
		if rulesSensitive(p) {
			sensitive = append(sensitive, RulesFile{p, true})
		} else {
			plain = append(plain, RulesFile{p, false})
		}
	}
	all := append(sensitive, plain...)
	if len(all) > rulesFilesShown {
		all = all[:rulesFilesShown]
	}
	return all, nil
}

// rulesSensitive marks the files that change what agents load or what runs on install: the global rules, the
// skills, hooks and the installer.
func rulesSensitive(path string) bool {
	if path == "claude/CLAUDE.md" || path == "install.sh" || strings.HasPrefix(path, "claude/skills/") {
		return true
	}
	for _, part := range strings.Split(path, "/") {
		if part == "hooks" || part == ".githooks" {
			return true
		}
	}
	return false
}

// untrackedCollisions lists the untracked files that a fast-forward from..to would overwrite or block.
func untrackedCollisions(ctx context.Context, e env, from, to string) []string {
	added, err := e.g(ctx, "diff", "--name-only", "-z", "--no-renames", "--diff-filter=A", from, to)
	if err != nil || added.Code != 0 {
		return []string{"(the update cannot be compared)"}
	}
	others, err := e.g(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil || others.Code != 0 {
		return []string{"(the worktree cannot be read)"}
	}
	var untracked []string
	for _, u := range strings.Split(others.Out, "\x00") {
		if u != "" {
			untracked = append(untracked, u)
		}
	}
	var hit []string
	for _, a := range strings.Split(added.Out, "\x00") {
		for _, u := range untracked {
			if a != "" && (a == u || strings.HasPrefix(a, u+"/") || strings.HasPrefix(u, a+"/")) {
				hit = append(hit, a)
				break
			}
		}
	}
	if len(hit) > 3 {
		hit = append(hit[:3], "...")
	}
	return hit
}

// rulesCommits reads all new commits. bad names, per offending commit, the role (author or committer) that is not
// allowed; the returned list is capped for display.
func rulesCommits(ctx context.Context, e env, from, to string, owners, committers []identity) (shown []RulesCommit, bad []string, err error) {
	out, gerr := e.g(ctx, "log", "--no-show-signature", "--no-mailmap", "--format=%H%x00%an%x00%ae%x00%cn%x00%ce%x00%s%x1e", from+".."+to)
	if gerr != nil || out.Code != 0 {
		return nil, nil, errors.New("the new commits cannot be listed")
	}
	for _, rec := range strings.Split(out.Out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x00")
		if len(f) != 6 || !oidPattern.MatchString(f[0]) {
			return nil, nil, errors.New("a new commit cannot be read")
		}
		oid := f[0]
		if !anyIdentity(owners, f[1], f[2]) {
			bad = append(bad, fmt.Sprintf("%s author %s <%s>", oid[:7], oneLine(f[1]), oneLine(f[2])))
		}
		if !anyIdentity(committers, f[3], f[4]) {
			bad = append(bad, fmt.Sprintf("%s committer %s <%s>", oid[:7], oneLine(f[3]), oneLine(f[4])))
		}
		if len(shown) < rulesCommitsShown {
			shown = append(shown, RulesCommit{OID: oid, Subject: oneLine(f[5]), Author: oneLine(f[1]) + " <" + oneLine(f[2]) + ">", Committer: oneLine(f[3]) + " <" + oneLine(f[4]) + ">"})
		}
	}
	return shown, bad, nil
}

// installKind looks (read-only) at home/.claude/CLAUDE.md: a symlink into the checkout, a copy (a file, or a
// symlink elsewhere) or absent.
func installKind(home, checkout string) string {
	if home == "" {
		return "absent"
	}
	link := filepath.Join(home, ".claude", "CLAUDE.md")
	info, err := os.Lstat(link)
	if err != nil {
		return "absent"
	}
	if info.Mode()&os.ModeSymlink != 0 {
		got, err1 := filepath.EvalSymlinks(link)
		want, err2 := filepath.EvalSymlinks(filepath.Join(checkout, "claude", "CLAUDE.md"))
		if err1 == nil && err2 == nil && got == want {
			return "symlink"
		}
	}
	return "copy"
}
