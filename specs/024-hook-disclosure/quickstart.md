# Quickstart: Hook and bypass disclosure

These steps use temporary repositories only and never touch a real project. They assume the implemented `stw hooks`; until then they describe the expected behavior.

## The WorkHarbor shape in plain Git

```sh
work=$(mktemp -d) && cd "$work"
git init -b main repo && cd repo
git config user.email you@example.org && git config user.name You
mkdir .githooks
for hook in pre-commit commit-msg pre-push; do
  printf '#!/bin/sh\necho "%s ran" >> "%s/sentinel"\n' "$hook" "$work" > ".githooks/$hook"
  chmod 755 ".githooks/$hook"
done
git add .githooks && git commit -m "add hooks" --no-verify
git config core.hooksPath .githooks

stw hooks
stw hooks --json
```

Expected: backend `git`; effective hooks path `.githooks` from the local configuration; `pre-commit`, `commit-msg` and `pre-push` active; for `commit` and `push` `runs_hooks: yes`, no bypass finding, status 0. Make a commit and push to a local bare remote and compare `sentinel` with the report; nothing in `stw hooks` itself writes `sentinel`.

## Silent disablement

- `chmod -x .githooks/pre-commit`: `hook-ignored`, `pre-commit` is `ignored`.
- `git config core.hooksPath /dev/null`: `hooks-path-null-device`, no directory hook active; `git config core.hooksPath nowhere`: `hooks-dir-absent`.
- `git config hook.mine.event pre-commit`: `config-hooks-not-evaluated` (the configuration hook is not evaluated).

## A colocated jj workspace (needs jj and its read-only sandbox)

```sh
cd "$work" && jj git init --colocate jjrepo && cd jjrepo
git config core.hooksPath .githooks   # reuse hooks created as above
cp -r ../repo/.githooks .
stw hooks
stw hooks --json | jq '.findings[].code'
```

Expected: backend `jj`; `jj commit` stated as `runs_hooks: no` at evidence level `documented`; `jj git push` as `runs_hooks: unknown` at `unverified`; `guards-bypassed-on-backend` (error), `guards-may-be-bypassed` and `backend-hook-behavior-unverified` (warnings); the exit status is 0 (findings never change it; read the JSON). After the conformance lane has recorded a measurement (verification note), the levels follow the recorded catalog.

## Measure jj yourself

`scripts/smoke-hooks.py` (conformance, after approval to build `bin/stw`) runs the reduced procedure (J0, J2, J2b, J3, J4, J7) of the spec. By hand: make the three hooks above write a sentinel, run `jj commit -m x` and `jj git push --bookmark b` against a local bare remote, and check whether `sentinel` exists. Plain Git in the same directory is the positive control.

## What the output does not prove

`active` means a file exists and is executable for you; it does not prove the script works, finds its interpreter or honors its own skip variables. `runs_hooks: no` at level `documented` is a vendor statement, not an observation. Per-command overrides such as `--no-verify` and caller environment variables are not detected.
