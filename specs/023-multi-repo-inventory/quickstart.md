# Quickstart: Multi-repository inventory

Temporary repositories only; never run this against real projects.

```sh
work=$(mktemp -d) && cd "$work"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
g="git -c user.name=t -c user.email=t@e -c init.defaultBranch=main -c protocol.file.allow=always"
$g init -q sub1 && (cd sub1 && $g commit -q --allow-empty -m s1)
$g init -q lib && (cd lib && $g commit -q --allow-empty -m l1 && echo dirty > f.txt)
$g init -q super && cd super && $g commit -q --allow-empty -m root
$g submodule add -q ../sub1 member1 && $g commit -qm member
$g worktree add -q ../wt-super -b topic && cd ..
$g -C super worktree add -q ../wt-gone -b gone && rm -rf wt-gone   # stale linked worktree
mkdir notrepo

stw repos super lib notrepo --submodules; echo "status $?"
stw repos super lib notrepo --submodules --json
```

Expected: entries for `super`, `lib`, `notrepo`, then `super/member1` (discovered, `origin: "submodule"`). `super` is `partial` (stale worktree `wt-gone`, counted as `stale` in its facts), `lib` is `ok` with `dirty: true`, `notrepo` is `error` (`not-repository`), overall `partial`, exit 1.

Other cases:

- `stw repos super super/member1/.` includes a duplicate path: the second is `duplicate-repository`.
- `stw repos super/member1/sub` (a subdirectory): `not-repository-root`.
- `git clone super clone` without `--recurse-submodules`, then `stw repos clone --submodules`: `clone/member1` is `skipped` with `submodule-uninitialized`.
- Replace `super/member1` by a symlink to `sub1` and rerun with `--submodules`: `path-unsafe`.
- With jj installed: `jj git init jjrepo`, then `stw repos jjrepo lib`: `jjrepo` has `dirty: unknown`.

Nothing is fetched or written. Clean up with `rm -rf "$work"`.
