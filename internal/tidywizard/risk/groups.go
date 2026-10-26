// SPDX-License-Identifier: EUPL-1.2
package risk

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

// Row supplies only presentation identity/effects and the evaluated risk.
// Path, Repo and ID are untrusted data; a later renderer must escape them.
type Row struct {
	ID, Kind, Repo, Path string
	Effects              []string
	Risk                 Assessment
}

// Group is a kind/effect/place view, never a selection or authorization.
// Key is an opaque stable ASCII-letter identity, not a path or a selector.
// Family is the leading lowercase ASCII prefix; Parent is used only for the
// workspace place. Repo is set for branch/tracking groups. Rows are sorted by ID.
// Consumers must keep decision-row selection rules independently of this view.
type Group struct {
	Key, Kind, Place, Family, Parent, Repo string
	Rows                                   []Row
	Level                                  Level
	Counts                                 Counts
}

// Family returns leading ASCII lowercase letters of the first component below
// /tmp, as written. It does not clean, resolve, inspect or execute the path.
func Family(p string) string {
	if !strings.HasPrefix(p, "/tmp/") {
		return ""
	}
	first := strings.SplitN(strings.TrimPrefix(p, "/tmp/"), "/", 2)[0]
	n := 0
	for n < len(first) && first[n] >= 'a' && first[n] <= 'z' {
		n++
	}
	return first[:n]
}

type groupIdentity struct{ Kind, Effects, Place, Family, Parent, Repo string }

// GroupRows groups only green/yellow/orange rows after the caller has narrowed
// the view. Families need three distinct choosable directory paths; scratchpad
// components take precedence. Effects normalize per-ref delete names to their
// effect class so branch groups remain per repository. Red/kept rows belong in
// the retain table. Groups sort by ascending worst risk, descending size, key.
func GroupRows(rows []Row) []Group {
	families := map[string]map[string]bool{}
	for _, r := range rows {
		if !groupable(r) || r.Kind != "worktree" || scratchpad(r.Path) {
			continue
		}
		if f := Family(r.Path); f != "" {
			if families[f] == nil {
				families[f] = map[string]bool{}
			}
			families[f][r.Path] = true
		}
	}
	groups := map[groupIdentity][]Row{}
	for _, r := range rows {
		if !groupable(r) {
			continue
		}
		identity := groupIdentity{Kind: r.Kind, Effects: effectClass(r.Effects)}
		switch r.Kind {
		case "branch", "tracking-ref":
			identity.Place = r.Kind
			identity.Repo = r.Repo
		default:
			switch {
			case scratchpad(r.Path):
				identity.Place = "scratchpads"
			case strings.HasPrefix(r.Path, "/tmp/"):
				identity.Place = "tmp-other"
				if f := Family(r.Path); f != "" && len(families[f]) >= 3 {
					identity.Place = "tmp-family"
					identity.Family = f
				}
			default:
				identity.Place = "workspace"
				identity.Parent = path.Dir(r.Path)
			}
		}
		r.Effects = append([]string{}, r.Effects...)
		r.Risk.Reasons = append([]string{}, r.Risk.Reasons...)
		groups[identity] = append(groups[identity], r)
	}
	out := []Group{}
	for identity, members := range groups {
		sort.Slice(members, func(i, j int) bool {
			a, b := members[i], members[j]
			if a.ID != b.ID {
				return a.ID < b.ID
			}
			if a.Repo != b.Repo {
				return a.Repo < b.Repo
			}
			return a.Path < b.Path
		})
		assessments := make([]Assessment, 0, len(members))
		for _, r := range members {
			assessments = append(assessments, r.Risk)
		}
		out = append(out, Group{Key: groupKey(identity), Kind: identity.Kind, Place: identity.Place, Family: identity.Family, Parent: identity.Parent, Repo: identity.Repo, Rows: members, Level: Worst(assessments), Counts: Count(assessments)})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Level != b.Level {
			return rank(a.Level) < rank(b.Level)
		}
		if len(a.Rows) != len(b.Rows) {
			return len(a.Rows) > len(b.Rows)
		}
		return a.Key < b.Key
	})
	return out
}
func groupable(r Row) bool {
	return r.Risk.Level == Green || r.Risk.Level == Yellow || r.Risk.Level == Orange
}
func scratchpad(p string) bool {
	for _, component := range strings.Split(p, "/") {
		if component == "scratchpad" {
			return true
		}
	}
	return false
}
func effectClass(in []string) string {
	seen := map[string]bool{}
	for _, effect := range in {
		if strings.HasPrefix(effect, "delete-branch:") {
			effect = "delete-branch"
		}
		if strings.HasPrefix(effect, "delete-tracking-ref:") {
			effect = "delete-tracking-ref"
		}
		seen[effect] = true
	}
	out := []string{}
	for effect := range seen {
		out = append(out, effect)
	}
	sort.Strings(out)
	return framed(out...)
}
func groupKey(identity groupIdentity) string {
	b := framed(identity.Kind, identity.Effects, identity.Place, identity.Family, identity.Parent, identity.Repo)
	digest := sha256.Sum256(append([]byte("stw-janitor-group\n"), b...))
	// Encode each hex nibble as a..p: a fixed-size key with no control bytes,
	// digits or raw untrusted text, even when two sanitized names would collide.
	key := []byte(hex.EncodeToString(digest[:]))
	for i, c := range key {
		if c <= '9' {
			key[i] = 'a' + c - '0'
		} else {
			key[i] = 'k' + c - 'a'
		}
	}
	return "group" + string(key)
}

// Length framing preserves raw Unix bytes; JSON string replacement would merge
// distinct non-UTF-8 paths/effects. These are internal identities, never output.
func framed(parts ...string) string {
	var b []byte
	for _, part := range parts {
		b = binary.AppendUvarint(b, uint64(len(part)))
		b = append(b, part...)
	}
	return string(b)
}
