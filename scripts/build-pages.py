#!/usr/bin/env python3
"""Stage SourceSteward's public Markdown and build the Hugo project site."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
from tempfile import TemporaryDirectory

ROOT = Path(__file__).resolve().parent.parent
PAGES = {
    "docs/status-and-sync.md": "/docs/status-and-sync/",
    "docs/cleanup-and-recovery.md": "/docs/cleanup-and-recovery/",
    "docs/worktrees-and-handoff.md": "/docs/worktrees-and-handoff/",
    "docs/review-and-watch.md": "/docs/review-and-watch/",
    "docs/development.md": "/docs/development/",
    "docs/migration-plan.md": "/docs/migration-plan/",
    "docs/oplog-format.md": "/docs/oplog-format/",
    "docs/command-reference.md": "/reference/",
    "llms.md": "/agents/",
    "brand/README.md": "/brand/",
}


def stage_markdown(site):
    for source, route in PAGES.items():
        first_line, _, body = (ROOT / source).read_text(encoding="utf-8").partition("\n")
        if not first_line.startswith("# "):
            raise ValueError(f"Expected a level-one heading in {source}")
        key = route.strip("/")
        target = site / "content" / (f"{key}.md" if key else "_index.md")
        target.parent.mkdir(parents=True, exist_ok=True)
        header = {"title": first_line.removeprefix("# "), "source": source, "url": route}
        target.write_text(json.dumps(header) + "\n" + body, encoding="utf-8")
    (site / "data").mkdir()
    (site / "data/routes.json").write_text(json.dumps({"README.md": "/", **PAGES}), encoding="utf-8")


def add_coverage_badge(site, report):
    summary = json.loads(report.read_text(encoding="utf-8"))["data"][0]["totals"]["lines"]
    total, covered = summary["count"], summary["covered"]
    if not (isinstance(total, int) and isinstance(covered, int) and 0 <= covered <= total and total > 0):
        raise ValueError("Expected a nonempty LLVM line coverage report")
    coverage = f"{covered / total:.1%}"
    badge = f"""<svg xmlns="http://www.w3.org/2000/svg" width="109" height="20" role="img" aria-label="coverage: {coverage}">
<title>Line coverage: {coverage}</title>
<rect width="109" height="20" rx="3" fill="#0b2545"/>
<path fill="#146c4b" d="M60 0h46a3 3 0 0 1 3 3v14a3 3 0 0 1-3 3H60z"/>
<g fill="#fff" font-family="Verdana,sans-serif" font-size="11" text-anchor="middle">
<text x="30" y="14">coverage</text><text x="84" y="14">{coverage}</text></g>
</svg>
"""
    (site / "static/coverage.svg").write_text(badge, encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--coverage", type=Path, help="cargo llvm-cov JSON summary")
    args = parser.parse_args()
    with TemporaryDirectory(prefix="stw-hugo-") as temporary:
        site = Path(temporary)
        shutil.copytree(ROOT / "site", site, dirs_exist_ok=True)
        shutil.copytree(ROOT / "docs/assets", site / "static/assets")
        shutil.copyfile(ROOT / "scripts/install.sh", site / "static/install.sh")
        stage_markdown(site)
        if args.coverage:
            add_coverage_badge(site, args.coverage)
        subprocess.run(
            ["hugo", "--source", str(site), "--destination", str(args.destination.resolve()),
             "--minify", "--panicOnWarning"],
            check=True,
        )


if __name__ == "__main__":
    main()
