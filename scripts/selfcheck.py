#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Pre-push self-check for authors.

Usage: python3 scripts/selfcheck.py [BASE] [--head REF] [--owner 'NAME <EMAIL>'] [--require-trailer]
Run it inside the worktree you are about to push. It checks the lines added by BASE...HEAD
(default BASE refs/remotes/origin/main, default HEAD the checked-out commit) for defect
classes that reviews keep finding:

  glued         words glued to numbers ("hidden015planner", "Exit0", "schema_version1"); code
                spans, fenced blocks, link targets, reference definitions and URLs are ignored
                (.md and .json files). Accepted patterns: pure hex tokens of 7+ characters,
                sha<N>sum, md5sum, base<N>[url], keyword spellings such as "row2" or "code128"
  blank         an added blank line directly after another blank line in markdown
  source        more than one "Source:" line (outside fences) in a chartroom/notes/*.md note
  diff-check    `git diff --check` whitespace errors
  identity      author or committer not equal to the owner, compared as the whole
                "NAME <EMAIL>" string; the owner is --owner or else the current git identity
                (read with `git config --get user.name` and `user.email`, never written);
                without either the check is skipped with an INFO line
  session-link  a claude.ai link or Claude-Session trailer in a commit message
  trailer       (only with --require-trailer) a commit without a Co-Authored-By trailer in its
                trailer block

INFO lines are not failures. "claims" lists added doc lines that name file:line, an exit
code or a flag, and "tick" compares ticked tasks with an "N/M done" statement; verify each
INFO line against the code yourself. "encoding" notes a file that is not valid UTF-8 (read
with replacement characters). Exit status: 0 no FAIL, 1 at least one FAIL, 2 usage or
environment error (not a git repository, unknown ref, no merge base such as in a shallow
clone, a failing git command). The tool is read-only: it never edits files and never writes
git configuration. Python standard library only.
"""
import argparse
import re
import subprocess
import sys

GIT = ["git", "-c", "core.quotepath=off", "-c", "diff.noprefix=false", "-c", "color.diff=false",
       "-c", "color.ui=false", "-c", "diff.renames=false", "-c", "diff.mnemonicprefix=false"]
DIFF = ["--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--inter-hunk-context=0"]

# Glued-word patterns. Digits directly between letters, or a keyword directly followed by digits.
BETWEEN = re.compile(r"\b[a-z]{3,}[0-9]+[a-z]{3,}\b", re.I)
KEYWORD = re.compile(r"\b(?:schema_version|exit|revision|rows|codes|version)[0-9]+\b", re.I)
ACCEPT = re.compile(r"^(?:sha[0-9]+sum|md5sum|base[0-9]+(?:url)?|[0-9a-f]{7,})$", re.I)
CLAIM = re.compile(r"(\b\w+\.\w+:\d+|exit (?:status |code )?\d|`--[a-z-]+`)")
OPEN_FENCE = re.compile(r"^\s*(`{3,}|~{3,})(.*)$")
NOTE = re.compile(r"(?:^|/)chartroom/notes/[^/]+\.md$")


class Fatal(Exception):
    pass


def git(*args, stdin=None):
    p = subprocess.run([*GIT, *args], capture_output=True, input=stdin)
    return p.returncode, p.stdout, p.stderr.decode("utf-8", "replace").strip()


def must(*args):
    rc, out, err = git(*args)
    if rc != 0:
        raise Fatal(f"git {' '.join(args[:2])} failed: {err or 'exit ' + str(rc)}")
    return out


def decode(b):
    return b.decode("utf-8", "replace")


def strip_code(s):
    s = re.sub(r"^\s*\[[^\]]+\]:\s*\S+", "", s)
    s = re.sub(r"`[^`]*`", "", s)
    s = re.sub(r"\]\([^)]*\)", "]", s)
    return re.sub(r"https?://\S+", "", s)


def glued(body):
    for rx in (BETWEEN, KEYWORD):
        for m in rx.finditer(body):
            if not ACCEPT.match(m.group(0)):
                yield m.group(0)


def hunk_rows(path, base, head):
    """Added lines (number, text) of one file, parsed only after its first hunk header."""
    out = must("diff", "-U0", *DIFF, f"{base}...{head}", "--", f":(literal){path}")
    rows, newno, inside = [], 0, False
    for line in out.split(b"\n"):
        if line.startswith(b"@@"):
            m = re.match(rb"@@ -\S+ \+(\d+)", line)
            newno, inside = (int(m.group(1)) if m else 0), True
        elif not inside or line.startswith(b"\\"):
            continue
        elif line.startswith(b"+"):
            rows.append((newno, line[1:]))
            newno += 1
        elif line.startswith(b" "):
            newno += 1
    return rows


def content(head, path):
    rc, out, _ = git("show", f"{head}:{path}")
    return out if rc == 0 else None


def lines_of(raw):
    return [l.rstrip("\r") for l in decode(raw).split("\n")]


def fenced(lines):
    """1-based numbers of lines on or inside a fenced code block (CommonMark open/close rules)."""
    marks, fence = set(), None
    for i, l in enumerate(lines, 1):
        if fence is None:
            m = OPEN_FENCE.match(l)
            if m and not (m.group(1)[0] == "`" and "`" in m.group(2)):
                fence = m.group(1)
                marks.add(i)
        else:
            marks.add(i)
            s = l.strip()
            if s and s[0] == fence[0] and len(s) >= len(fence) and set(s) == {fence[0]}:
                fence = None
    return marks


def main(argv):
    ap = argparse.ArgumentParser(prog="selfcheck.py", description="Pre-push self-check for authors.")
    ap.add_argument("base", nargs="?", default="refs/remotes/origin/main")
    ap.add_argument("--head", default="HEAD")
    ap.add_argument("--owner", help="expected author and committer as 'NAME <EMAIL>' "
                    "(default: the current git identity)")
    ap.add_argument("--require-trailer", action="store_true",
                    help="fail on a commit without a Co-Authored-By trailer")
    a = ap.parse_args(argv)
    base, head = a.base, a.head
    try:
        return run(a, base, head)
    except Fatal as e:
        print(f"selfcheck: {e}", file=sys.stderr)
        return 2


def run(a, base, head):
    if git("rev-parse", "--git-dir")[0] != 0:
        raise Fatal("not inside a git repository")
    if git("rev-parse", "--verify", "--quiet", f"{base}^{{commit}}")[0] != 0:
        raise Fatal(f"base ref {base} does not exist; fetch it first (for example "
                    "`git fetch <url> +refs/heads/main:refs/remotes/origin/main`) or pass another BASE")
    if git("rev-parse", "--verify", "--quiet", f"{head}^{{commit}}")[0] != 0:
        raise Fatal(f"head {head} does not exist")
    if git("merge-base", base, head)[0] != 0:
        raise Fatal(f"no merge base between {base} and {head} (shallow clone or unrelated history? "
                    "fetch more history, for example `git fetch --unshallow`, or pass another BASE)")

    owner = a.owner
    if not owner:
        name = decode(git("config", "--get", "user.name")[1]).strip()
        email = decode(git("config", "--get", "user.email")[1]).strip()
        owner = f"{name} <{email}>" if name and email else None

    failed = False

    def fail(kind, where, msg):
        nonlocal failed
        failed = True
        print(f"FAIL {kind}: {where}: {msg}")

    def info(kind, where, msg):
        print(f"INFO {kind}: {where}: {msg}")

    names = [decode(n) for n in must("diff", "--name-only", "-z", *DIFF, f"{base}...{head}").split(b"\0") if n]
    rows_of, raw_of = {}, {}
    for f in names:
        rows_of[f] = [(n, decode(t).rstrip("\r")) for n, t in hunk_rows(f, base, head)]
        raw_of[f] = content(head, f)
        try:
            (raw_of[f] or b"").decode("utf-8")
        except UnicodeDecodeError:
            info("encoding", f, "not valid UTF-8; read with replacement characters")

    for f in names:
        if not f.endswith((".md", ".json")) or raw_of[f] is None:
            continue
        md = f.endswith(".md")
        full = lines_of(raw_of[f])
        skip = fenced(full) if md else set()
        for n, t in rows_of[f]:
            if n in skip:
                continue
            for word in glued(strip_code(t)):
                fail("glued", f"{f}:{n}", f"'{word}'")
            if md:
                if t.strip() == "" and n >= 2 and full[n - 2].strip() == "":
                    fail("blank", f"{f}:{n}", "two consecutive blank lines")
                if CLAIM.search(t) and not t.startswith("|"):
                    info("claims", f"{f}:{n}", t.strip()[:110])

    for tf in [f for f in names if f.endswith("tasks.md") and raw_of[f] is not None]:
        lines = lines_of(raw_of[tf])
        done = sum(1 for l in lines if l.startswith("- [x]"))
        total = done + sum(1 for l in lines if l.startswith("- [ ]"))
        for pf in [f for f in names if f.endswith("progress.md")]:
            for n, t in rows_of[pf]:
                for m in re.finditer(r"(\d+)/(\d+) done", t):
                    if (int(m.group(1)), int(m.group(2))) != (done, total):
                        info("tick", f"{pf}:{n}",
                             f"says {m.group(0)}; {tf} has {done}/{total} ticked (ignore if another spec)")

    for f in names:
        if NOTE.search(f) and raw_of[f] is not None:
            lines = lines_of(raw_of[f])
            skip = fenced(lines)
            n_src = sum(1 for i, l in enumerate(lines, 1) if l.startswith("Source:") and i not in skip)
            if n_src > 1:
                fail("source", f, f"{n_src} Source: lines")

    rc, out, err = git("diff", "--check", f"{base}...{head}")
    if rc != 0:
        if not out.strip():
            raise Fatal(f"git diff --check failed: {err or 'exit ' + str(rc)}")
        fail("diff-check", "-", decode(out).strip()[:200])

    if owner is None:
        info("identity", "-", "skipped: pass --owner 'NAME <EMAIL>' or set user.name and user.email")
    for h in decode(must("log", "--format=%H", f"{base}..{head}")).split():
        rec = decode(must("log", "-1", "--format=%an <%ae>%x00%cn <%ce>%x00%B", h)).split("\0", 2)
        an, cn, body = rec[0], rec[1], rec[2]
        if a.require_trailer:
            rc, tr, _ = git("interpret-trailers", "--parse", stdin=body.encode())
            if not re.search(r"^co-authored-by:\s*\S", decode(tr), re.I | re.M):
                fail("trailer", h[:8], "no Co-Authored-By trailer")
        if re.search(r"claude\.ai|Claude-Session", body):
            fail("session-link", h[:8], "session link in message")
        if owner is not None and (an != owner or cn != owner):
            fail("identity", h[:8], f"{an} | {cn} (expected {owner})")

    print("selfcheck: FAILED" if failed else "selfcheck: ok (verify the INFO claims against the code yourself)")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
