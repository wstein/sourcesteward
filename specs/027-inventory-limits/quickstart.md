# Quickstart: Inventory limits and checkout description

All steps use temporary repositories only. They need an `stw` binary; building one is a build step that AGENTS.md reserves for the maintainer's approval, so the author of this spec did not run them with the new code (the "before" results are reproduced in research.md Measurement 1 through the Go test harness). Use `git` 2.46 or newer and `python3`.

```sh
work="$(mktemp -d)" && cd "$work"
export GIT_CONFIG_GLOBAL=/dev/null GIT_AUTHOR_NAME=T GIT_AUTHOR_EMAIL=t@example.org GIT_COMMITTER_NAME=T GIT_COMMITTER_EMAIL=t@example.org

# A repository with 5,000 tracked files.
git init -q -b main big && cd big
python3 - <<'PY'
import os
for i in range(5000):
    d = "d%03d" % (i // 50)
    os.makedirs(d, exist_ok=True)
    open("%s/file%05d.txt" % (d, i), "w").write("content %d\n" % i)
PY
git add -A && git commit -qm init
stw inventory . --json | python3 -c 'import json,sys; r=json.load(sys.stdin); print(r["status"], r["workspaces"][0]["tracked"], len(r["files"]))'
```

Expected after this feature: `ok {'value': 'false', 'reason': ''} 0`. Before: `error` with `git-inventory-failed`.

```sh
echo more >> d000/file00001.txt; echo new > untracked.txt
stw inventory . --json | python3 -c 'import json,sys; r=json.load(sys.stdin); print(r["status"], sorted((f["scope"], f["path"]) for f in r["files"]))'
```

Expected: `ok` and three records: `disk` and `tracked` for `d000/file00001.txt`, `untracked` for `untracked.txt`.

```sh
# Many tags: tags are omitted first, predicates still see all of them.
for i in $(seq 1100); do echo "create refs/tags/t$i HEAD"; done | git update-ref --stdin
stw inventory . --json | python3 -c 'import json,sys; r=json.load(sys.stdin); print(r["status"], len(r["references"]), [l for l in r["boundary"]["limitations"] if "reference" in l])'
stw inventory . --query ref-exists --ref refs/tags/t1100 --json | python3 -c 'import json,sys; r=json.load(sys.stdin); print(r["status"], r["query"]["value"])'
```

Expected: `partial`, at most about 510 references (tags cost two records each, tags are omitted first), the TXT-2 limitation naming the omitted count; then `ok true`.

```sh
# A submodule checkout and a gitlink-bearing superproject.
cd "$work" && git init -q -b main lib && (cd lib && echo a > a && git add a && git commit -qm lib)
git init -q -b main super && cd super && echo s > s && git add s && git commit -qm s
git -c protocol.file.allow=always submodule add -q "$work/lib" mods/lib && git commit -qm sub
stw inventory mods/lib --json | python3 -c 'import json,sys; r=json.load(sys.stdin); w=r["workspaces"][0]; print(w["path"].endswith("super/mods/lib"), w["tracked"]["value"])'
echo x >> s
stw inventory . --json | python3 -c 'import json,sys; r=json.load(sys.stdin); print(r["status"], r["workspaces"][0]["tracked"]["value"], r["workspaces"][0]["limitations"])'
git checkout -q -- s
stw inventory . --json | python3 -c 'import json,sys; r=json.load(sys.stdin); print(r["status"], r["workspaces"][0]["tracked"])'
```

Expected: `True false` (before: the path ends with `.git/modules/mods/lib` and `tracked` is `true`); then `partial true ['submodule disk state not observed; tracked is true from staged or ordinary-file evidence only']` (partial because the submodule worktree is unobserved; `stw inventory . --query dirty --json` gives `ok` and `true`); then `partial` and `tracked` unknown with `submodule disk state excluded to avoid recursive helpers`.

```sh
# Effects: nothing changes.
find . -type f -not -path './mods/lib/.git' -exec sha256sum {} + | sort > /tmp/before.$$ ; stw inventory . --json >/dev/null
find . -type f -not -path './mods/lib/.git' -exec sha256sum {} + | sort | diff - /tmp/before.$$ && echo unchanged; rm -f /tmp/before.$$
```

Expected: `unchanged` (the index stat information is not rewritten; `ls -l --time=ctime .git/index` is the same before and after).
