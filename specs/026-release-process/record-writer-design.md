# WI4 record writer: scoped portability design candidate

Status: proposed; not authorization to implement the Darwin adapter. The existing
Linux-only writer is provisional. T022 and WI4 remain incomplete until this design,
the implementation and their exact source heads receive independent review. This addendum
is incorporated by the controlling implementation plan; the proposed exception remains
implementation-gated until the independent design review is CLEAR.

## Requirement and observed gap

[record-command.md](contracts/record-command.md#effects) requires descriptor-relative
`openat` with `O_NOFOLLOW` for every component, exclusive creation and an identical-file
no-op. The Go 1.27.2 `os.Root` documentation says that its methods follow symbolic links;
`Root.OpenRoot` cannot atomically reject a component that becomes a symlink. A sequence
of `Lstat` and `OpenRoot` checks therefore cannot establish the required invariant.
The standard `syscall` package exports `Openat` and `Mkdirat` on Linux, but not Darwin.
The already pinned `golang.org/x/sys v0.47.0` exports both for Linux and Darwin. These
statements were checked against the locally installed Go documentation and cached
module source; no download, build or native Darwin execution was performed.

The [plan](plan.md) presently permits no new runtime dependency, and the release AST
and Python architecture checks reject every `x/sys` import. The proposed change is a
narrow exception for an existing pinned module. It does not relax the process or
network boundary or permit release-package foreign source, cgo, unsafe, assembly or
linkname directives. The existing pinned dependency's platform syscall wrappers are
the reviewed filesystem adapter's implementation dependency, like the standard
library's platform wrappers; they do not permit those mechanisms in project source.

## Proposed exact exception

Replace the provisional Linux-only writer with exactly one production adapter,
`internal/release/openfile_write_unix.go`, build-tagged `linux || darwin`.
Only that exact file may import `golang.org/x/sys/unix`, without an alias, dot or blank
name. Its only allowed `unix` selector identifiers are `Openat`, `Mkdirat` and the
explicit constants `O_RDONLY`, `O_WRONLY`, `O_CREAT`, `O_EXCL`, `O_NOFOLLOW`,
`O_DIRECTORY`, `O_CLOEXEC`, and `O_NONBLOCK`. Every reference to the imported package,
including passing a selector as a value, is examined by the AST checker. `unix.Openat`
and `unix.Mkdirat` are allowed only when the exact selector expression is the `Fun`
of its immediate `ast.CallExpr`. Capturing, assigning, returning or passing either
function, or storing it in a composite literal, is forbidden. Parentheses, aliases
and generic forwarding must not turn a function value into a permitted direct call.
Other references to the package identifier are rejected; function-local shadowing
does not grant an exception. The adapter may not export a function-valued handle or
a generic syscall forwarding helper. Review traces its private helper callers. Importing
that dependency elsewhere or using another selector fails the architecture checks.
The Python checker independently enforces the same exact file/import/selector rule;
the existing process and network checks remain. The new closed dependency guard below
rejects unrelated external helper routes. The module
version stays v0.47.0; only its indirect/direct classification changes after tidy.
No raw syscall, dependency upgrade, unsafe wrapper or broad syscall permission is added.

The dependency guard is an additional restriction, not a claim about the old guard.
Every direct import in `internal/release` and its subpackages must be either a Go
standard-library package, the root embedded-schema package, the exact existing
`internal/inspection` or `internal/hosting` operation packages, another release
subpackage, or the exact writer's pinned `golang.org/x/sys/unix` exception. An
arbitrary external package is rejected even if it exposes no network or process API.
The dependency graph, excluding the `internal/hosting` boundary, also admits no
non-standard package other than those project packages and `golang.org/x/sys/unix`;
that pinned package currently imports only standard-library packages on both Linux
and Darwin. A new foreign helper under inspection or any other route therefore
fails closed. The actual Go package metadata distinguishes standard-library
packages from external packages; a package-name heuristic is not the authority.
The AST source check enforces direct imports and the Python graph check enforces
the closure, with independent synthetic helper-route mutants. Existing transitive
network and process checks remain active outside hosting. Existing bans on unsafe,
cgo, project assembly and linkname remain scoped to project release source. No
blanket unsafe/assembly prohibition is claimed for standard-library dependencies or
the pinned unix package, whose legitimate platform wrappers use those facilities.

The adapter uses standard `os.File` ownership/close methods around the resulting file
descriptors. Its root descriptor is opened with directory/no-follow flags. Each
single validated component is opened relative to the current pinned descriptor with
`O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC`; only `ENOENT` may cause `Mkdirat`, followed by
the same guarded open. Concurrent `EEXIST` after `Mkdirat` is followed by that guarded
open. A symlink, non-directory, access error or race cannot fall back to a pathname.
The old directory descriptor is closed only after the new one is acquired.
Descriptors bind directory inodes: a concurrent ancestor rename cannot redirect a
subsequent open into a replacement symlink or directory. A rename can relocate the
originally opened directory inode; this mechanism does not lock directory names.
The race tests record that exact inode behavior, including an ancestor renamed
within the fixture root followed by a hostile replacement. They must not claim
that descriptor pinning freezes a pathname or prevents another actor relocating
a directory. Any unmet working-tree effect requirement discovered by that test
is a design blocker, not an acceptance waiver.

The final validated basename is opened relative to the pinned parent with
`O_CREAT | O_EXCL | O_WRONLY | O_NOFOLLOW | O_CLOEXEC`, mode 0644; only the newly
created file may be written or have its mode set. An existing destination is opened
with `O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC`, must be regular, and is compared
within the canonical record length plus one byte. Equal bytes are a no-op; unequal
bytes or any other file type refuse `output-exists`. Existing files are never changed.
Only the product record and absent `docs/releases` path directories may be created.
No staging, native VCS mutation, process execution, network or artifact execution is
introduced. An interrupted write can leave only that new partial record; removing it
undoes the file effect. Unsupported platforms refuse writing before creating anything.

## Verification required before acceptance

1. Failing-first AST and Python mutants: the exact adapter's intended calls admitted;
   the same calls in any other file denied; alias/dot/blank import denied; process,
   raw syscall, unsafe, cgo, assembly, network, package-as-value and non-allowlisted
   selector mutations denied. For each of Openat and Mkdirat, use valid Go examples
   of assignment, capture, return, argument passing and composite-literal storage;
   all fail even though the selectors themselves are otherwise permitted. Add
   release → foreign helper → syscall/unsafe/network/process graph mutants and
   release subpackage → foreign helper variants. The pinned adapter is the only
   additional external route. Existing architecture evasion corpus continues to pass.
2. Native Linux writer fixtures: new/identical/different files, each symlinked path
   component, final symlink/directory/FIFO/device, named products, creation mode,
   hostile components, and deterministic before-open/after-open component swaps at
   every directory depth, including concurrent creation and ancestor renames. Add
   hardlink destinations (identical no-op and different refusal), oversized existing
   content, concurrent existing-file mutation, short-write/interruption injection,
   descriptor-close error/cleanup, restrictive umask and mode 0644 verification.
   Only direct adapter calls execute; test synchronization never exposes syscall
   function values or a generic forwarding escape. The symlink target remains
   unchanged. Existing file identity/content/mode and repository sentinels stay intact.
3. Portable source tests, race tests, vet, module verification and architecture checks.
   Test compiler verification of the writer for darwin/amd64 and darwin/arm64, without
   running those executables or making product artifacts. Native Darwin execution is
   recorded as unrun unless an authorized existing lane supplies actual evidence.
4. Independent review of the exact implementation SHA after the design review is CLEAR.
   No claim of native Darwin security or complete cross-platform acceptance follows
   from compilation alone. T022's implementation and native acceptance limits stay
   explicit in WI4 verification evidence.
