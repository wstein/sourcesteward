# Quickstart: Submodule pin audit

These steps use temporary repositories only and never touch a real project.

## The incident, reproduced

```sh
work=$(mktemp -d) && cd "$work"
git init --bare sub-remote.git
git init sub && cd sub && git commit --allow-empty -m one && git remote add origin ../sub-remote.git && git push origin HEAD:main && cd ..
git init super && cd super
git -c protocol.file.allow=always submodule add ../sub-remote.git member
git commit -m "add member"
# a commit that is only local, then pin it
git -C member commit --allow-empty -m local-only
git add member && git commit -m "pin a local-only commit"

stw pins
stw pins --json
stw pins --strict; echo "status $?"
```

Expected: `member` reports a pin whose commit is `unreachable`, the finding `pin-unpublished` at error severity, status 0 without `--strict` and 3 with it. Push the submodule commit (`git -C member push origin HEAD:main`), run again, and publication becomes `reachable` only after the remote-tracking ref has been updated by the push; `stw pins` never fetches.

## Other cases

- Stage a different pin (`git -C member checkout <other>` then `git add member`) and edit nothing else: `staged-pin-differs`.
- Leave a submodule uninitialized (`git clone` without `--recurse-submodules`): `submodule-uninitialized`, publication `unknown`.
- Make a jj colocated checkout of the superproject: `stw pins` refuses with `unsupported-backend`.

## What the output does not prove

`unreachable` means local remote-tracking refs do not contain the pin. It does not prove the remote lacks the commit. Run a fetch yourself and audit again if you need fresher evidence.
