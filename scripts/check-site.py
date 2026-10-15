#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Validate generated public HTML and its exact source revision offline."""
from html.parser import HTMLParser
import importlib.util
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

BASE = '/sourcesteward/'
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('authored_docs', ROOT / 'scripts/check-docs.py')
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)
package_spec = importlib.util.spec_from_file_location('schema_publication', ROOT / 'scripts/package-pages.py')
publication = importlib.util.module_from_spec(package_spec)
package_spec.loader.exec_module(publication)

class Links(HTMLParser):
    def __init__(self):
        super().__init__()
        self.targets = []
    def handle_starttag(self,tag,attrs):
        attrs = dict(attrs)
        for key in ('href','src'):
            if key in attrs: self.targets.append((attrs[key],tag in ('script','img','link')))

def validate(destination,revision,source_root=ROOT):
    if (destination / 'revision.txt').read_text() != revision + '\n': raise ValueError('Artifact source revision mismatch')
    for forbidden in ('specs','internal','.git','docs/progress.md','docs/harness.md','AGENTS.md'):
        if (destination / forbidden).exists(): raise ValueError('Private source leaked: ' + forbidden)
    pages = list(destination.rglob('*.html'))
    for path in pages:
        text = path.read_text()
        page = checker.Page(theme_script=BASE + 'assets/theme.js')
        page.feed(text)
        if not (page.has_main and page.has_lang and page.has_viewport and page.theme_scripts == 1): raise ValueError('Missing accessible page shell: ' + str(path))
        if not set(page.anchors).issubset(page.ids): raise ValueError('Broken local anchor: ' + str(path))
        links = Links(); links.feed(text)
        for target,runtime in links.targets:
            url = urlsplit(target)
            if url.scheme or url.netloc:
                if runtime: raise ValueError('External runtime asset: ' + target)
                if url.scheme not in ('https','http','mailto'): raise ValueError('Unsafe URL scheme: ' + target)
                continue
            if not url.path: continue
            if url.path.startswith('/'):
                if not url.path.startswith(BASE): raise ValueError('Project base path lost: ' + target)
                local = destination / unquote(url.path[len(BASE):])
            else: local = path.parent / unquote(url.path)
            local = local.resolve()
            if not local.is_relative_to(destination.resolve()): raise ValueError('Link escapes artifact: ' + target)
            if local.is_dir(): local /= 'index.html'
            if not local.is_file(): raise ValueError('Broken generated link: ' + target)
            if url.fragment and local.suffix == '.html':
                referenced = checker.Page(theme_script=BASE + 'assets/theme.js'); referenced.feed(local.read_text())
                if unquote(url.fragment) not in referenced.ids: raise ValueError('Broken generated fragment: ' + target)
    sources = publication.schema_sources(source_root)
    publication.schema_sources(destination)
    for source in sources:
        target = destination / 'schemas' / source.relative_to(source_root / 'schemas')
        if target.read_bytes() != source.read_bytes(): raise ValueError('Schema bytes changed')
    print(f'Generated site checks passed ({len(pages)} HTML pages; revision {revision}).')

if __name__ == '__main__':
    if len(sys.argv) != 3 or not re.fullmatch('[0-9a-f]{40}',sys.argv[2]): raise SystemExit('usage: check-site.py SITE FULL_SHA')
    validate(Path(sys.argv[1]),sys.argv[2])
