// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"
)

const (
	pinsJJMessage          = "jj has no Git submodule model; run this in a Git-owned checkout"
	pinsButlerMessage      = "GitButler-managed workspaces have no supported submodule audit; run this in a Git-owned checkout"
	pinsAmbiguousMessage   = "ownership markers conflict (jj and GitButler); resolve the ownership before auditing pins"
	pinsBareMessage        = "a bare repository has no worktree to compare with its submodule pins"
	pinsNoRepoMessage      = "no Git repository at the path"
	pinsNoDirectoryMessage = "no Git repository at the path: it does not exist or is not a directory"
)

// Pins audits the submodule pins of a Git superproject without writes, network or fetch.
func Pins(ctx context.Context, options PinsOptions) PinsResult { return pins(ctx, options, runGit) }

// pins decides ownership first and never reads submodule state before that decision:
//  1. jj-owned (standalone, colocated, secondary) and conflicting markers are decided from filesystem
//     markers alone and start no process of any kind;
//  2. every other decision uses the existing Git detection, which starts only Git processes and never jj.
func pins(ctx context.Context, options PinsOptions, run runner) PinsResult {
	shown := options.Path
	if shown == "" {
		shown = "."
	}
	// The backend stays unknown until ownership is decided, so an early failure never claims Git.
	r := NewPinsResult(shown)
	finish := func() PinsResult {
		if err := r.Finalize(); err != nil {
			r.Status, r.Submodules, r.Findings = "error", []PinsSubmodule{}, []PinsFinding{}
			r.Error = &PinsError{Code: "pins-failed", Message: err.Error()}
			_ = r.Finalize()
		}
		return r
	}
	fail := func(message string) PinsResult {
		r.Status, r.Error = "error", &PinsError{Code: "pins-failed", Message: message}
		r.Identity, r.Submodules, r.Findings = nil, []PinsSubmodule{}, []PinsFinding{}
		return finish()
	}
	refuse := func(backend, code, message string) PinsResult {
		r.Status, r.Backend, r.Error = "refused", backend, &PinsError{Code: code, Message: message}
		return finish()
	}
	if err := ValidatePinsOptions(options); err != nil {
		return fail(err.Error())
	}
	if ctx.Err() != nil {
		return fail("pins canceled")
	}
	abs, err := filepath.Abs(shown)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return refuse("unknown", "not-repository", pinsNoDirectoryMessage)
	}
	if err != nil {
		return fail("cannot access the pins path")
	}
	if !utf8.ValidString(abs) {
		return fail("the resolved pins path is not valid UTF-8")
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return refuse("unknown", "not-repository", pinsNoDirectoryMessage)
		}
		return fail("cannot access the pins path")
	}
	var markers Result
	if _, err := ancestors(abs, &markers); err != nil {
		return fail("cannot inspect backend metadata")
	}
	if len(markers.Evidence) > 0 {
		if err := detectJJGitButler(ctx, abs, &markers); err != nil {
			if ctx.Err() != nil {
				return fail("pins canceled")
			}
			return fail("cannot safely inspect colocated ownership metadata")
		}
		managed(&markers)
		if markers.Backend == "ambiguous" {
			return refuse("ambiguous", "ambiguous-backend", pinsAmbiguousMessage)
		}
		return refuse("jj", "unsupported-backend", pinsJJMessage)
	}
	identity := inspectOwned(ctx, abs, run, true)
	if ctx.Err() != nil {
		return fail("pins canceled")
	}
	switch identity.Status {
	case "unsupported":
		if identity.Backend == "ambiguous" {
			return refuse("ambiguous", "ambiguous-backend", pinsAmbiguousMessage)
		}
		return refuse(identity.Backend, "unsupported-backend", pinsButlerMessage)
	case "error":
		if len(identity.Diagnostics) > 0 && identity.Diagnostics[0].Code == "not-repository" {
			return refuse("unknown", "not-repository", pinsNoRepoMessage)
		}
		return fail("cannot establish repository identity: " + identity.Error())
	}
	// Ownership is established: the backend is Git from here on.
	r.Backend = "git"
	if identity.Repository != nil && identity.Repository.Bare {
		return refuse("git", "bare-repository", pinsBareMessage)
	}
	if err := pinsAudit(ctx, abs, identity, &r); err != nil {
		if ctx.Err() != nil {
			return fail("pins canceled")
		}
		return fail("cannot audit the submodule pins: " + err.Error())
	}
	return finish()
}
