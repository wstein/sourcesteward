#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Pre-push self-check for authors.

Usage: python3 scripts/selfcheck.py [BASE] [--head REF] [--owner NAME]
Run it inside the worktree you are about to push. It checks the lines added by BASE...HEAD
(default BASE refs/remotes/origin/main, default HEAD the checked-out commit) for defect
classes that reviews keep finding:

  glued       words glued to numbers ("hidden015planner", "schema_version1"); code spans,
              fenced blocks, link targets and URLs are ignored (.md and .json files)
  blank       an added blank line directly after another blank line in markdown
  source      more than one "Source:" line in a chartroom note (chartroom/notes/)
  diff-check  `git diff --check` whitespace errors
  trailer     a commit without a Co-Authored-By trailer
  identity    author or committer other than the owner (default "Werner Stein")
  session-link  a claude.ai link or Claude-Session trailer in a commit message

INFO lines are not failures. "claims" lists added doc lines that name file:line, an exit
code or a flag, and "tick" compares ticked tasks with an "N/M done" statement; verify each
INFO line against the code yourself. Exit status: 0 no FAIL, 1 at least one FAIL, 2 usage
error or a base ref that does not exist. The tool is read-only: it never edits files and
never writes git configuration. Python standard library only.
"""
import argparse
import re
import subprocess
import sys

GLUE = [
    re.compile(r"\b[a-z]{3,}[0-9]{2,}[a-z]{3,}\b"),
    re.compile(r"\b(?:schema_version|exit|revision|row|rows|code|codes|version)[0-9]+\b"),
]
CLAIM = re.compile(r"(\b\w+\.\w+:\d+|exit (?:status |code )?\d|`--[a-z-]+`)")
FENCE = re.compile(r"^\s*(```|~~~)")


def git(*args):
    p = subprocess.run(["git", *args], capture_output=True, text=True)
    return p.returncode, p.stdout, p.stderr


def strip_code(s):
    s = re.sub(r"`[^`]*`", "", s)
    s = re.sub(r"\]\([^)]*\)", "]", s)
    return re.sub(r"https?://\S+", "", s)


def added_lines(base, head):
    files, cur, newno = {}, None, 0
    for line in git("diff", "-U0", f"{base}...{head}")[1].splitlines():
        if line.startswith("+++ "):
            cur = line[6:] if line.startswith("+++ b/") else None
            if cur is not None:
                files[cur] = []
        elif line.startswith("@@"):
            m = re.search(r"\+(\d+)", line)
            newno = int(m.group(1)) if m else 0
        elif line.startswith("+") and cur is not None:
            files[cur].append((newno, line[1:]))
            newno += 1
    return files


def content(head, path):
    rc, out, _ = git("show", f"{head}:{path}")
    return out.splitlines() if rc == 0 else None


def fenced(lines):
    """Line numbers (1-based) inside or on a fenced code block."""
    inside, marks = False, set()
    for i, l in enumerate(lines, 1):
        if FENCE.match(l):
            marks.add(i)
            inside = not inside
        elif inside:
            marks.add(i)
    return marks


def main(argv):
    ap = argparse.ArgumentParser(prog="selfcheck.py", description="Pre-push self-check for authors.")
    ap.add_argument("base", nargs="?", default="refs/remotes/origin/main")
    ap.add_argument("--head", default="HEAD")
    ap.add_argument("--owner", default="Werner Stein")
    a = ap.parse_args(argv)
    base, head = a.base, a.head

    if git("rev-parse", "--git-dir")[0] != 0:
        print("selfcheck: not inside a git repository", file=sys.stderr)
        return 2
    for ref in (base, head):
        if git("rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}")[0] != 0:
            print(f"selfcheck: base ref {ref} does not exist; fetch it first "
                  f"(for example `git fetch <url> +refs/heads/main:refs/remotes/origin/main`) "
                  f"or pass another BASE" if ref == base else f"selfcheck: head {ref} does not exist",
                  file=sys.stderr)
            return 2

    failed = False

    def fail(kind, where, msg):
        nonlocal failed
        failed = True
        print(f"FAIL {kind}: {where}: {msg}")

    def info(kind, where, msg):
        print(f"INFO {kind}: {where}: {msg}")

    files = added_lines(base, head)
    for f, rows in files.items():
        if not f.endswith((".md", ".json")):
            continue
        full = content(head, f) or []
        skip = fenced(full) if f.endswith(".md") else set()
        for n, t in rows:
            if n in skip:
                continue
            body = strip_code(t)
            for rx in GLUE:
                for m in rx.finditer(body):
                    fail("glued", f"{f}:{n}", f"'{m.group(0)}'")
            if f.endswith(".md"):
                if t.strip() == "" and n > 1 and n - 1 <= len(full) and full[n - 2].strip() == "":
                    fail("blank", f"{f}:{n}", "two consecutive blank lines")
                if CLAIM.search(t) and not t.startswith("|"):
                    info("claims", f"{f}:{n}", t.strip()[:110])

    for tf in [f for f in files if f.endswith("tasks.md")]:
        lines = content(head, tf) or []
        done = sum(1 for l in lines if l.startswith("- [x]"))
        total = done + sum(1 for l in lines if l.startswith("- [ ]"))
        for pf in [f for f in files if f.endswith("progress.md")]:
            for n, t in files[pf]:
                for m in re.finditer(r"(\d+)/(\d+) done", t):
                    if (int(m.group(1)), int(m.group(2))) != (done, total):
                        info("tick", f"{pf}:{n}",
                             f"says {m.group(0)}; {tf} has {done}/{total} ticked (ignore if another spec)")

    for f in files:
        if "chartroom/notes/" in f:
            lines = content(head, f)
            if lines is None:
                continue
            n_src = sum(1 for l in lines if l.startswith("Source:"))
            if n_src != 1:
                fail("source", f, f"{n_src} Source: lines")

    rc, out, _ = git("diff", "--check", f"{base}...{head}")
    if rc:
        fail("diff-check", "-", out.strip()[:200])

    for h in git("log", "--format=%H", f"{base}..{head}")[1].split():
        body = git("log", "-1", "--format=%B", h)[1]
        an = git("log", "-1", "--format=%an <%ae>", h)[1].strip()
        cn = git("log", "-1", "--format=%cn <%ce>", h)[1].strip()
        if "co-authored-by:" not in body.lower():
            fail("trailer", h[:8], "no Co-Authored-By trailer")
        if re.search(r"claude\.ai|Claude-Session", body):
            fail("session-link", h[:8], "session link in message")
        if a.owner not in an or a.owner not in cn:
            fail("identity", h[:8], f"{an} | {cn}")

    print("selfcheck: FAILED" if failed else "selfcheck: ok (verify the INFO claims against the code yourself)")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
