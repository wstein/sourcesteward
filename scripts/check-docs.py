#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Offline validation of authored docs, site assets and harness records."""
from html.parser import HTMLParser
import json
from pathlib import Path
import re
from urllib.parse import unquote, urlsplit
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]


def local_target(root, source, target):
    url = urlsplit(target)
    if url.scheme or url.netloc or not url.path:
        return
    path = (source.parent / unquote(url.path)).resolve()
    if not path.is_relative_to(root.resolve()) or not path.exists():
        raise ValueError(f'{source.relative_to(root)}: broken local link {target}')


def check_asset(root, source, target):
    url = urlsplit(target)
    if url.scheme or url.netloc:
        raise ValueError('Runtime assets must be local: ' + target)
    local_target(root, source, target)


def check_links(root, source):
    text = source.read_text()
    for target in re.findall(r'!?\[[^\]]*\]\(([^\s)]+)\)', text):
        local_target(root, source, target)
    for target in re.findall(r'(?:src|href)="([^"]+)"', text):
        local_target(root, source, target)


class Page(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids = set()
        self.anchors = []
        self.has_main = False
        self.has_lang = False
        self.has_viewport = False

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if 'id' in attrs:
            if attrs['id'] in self.ids:
                raise ValueError('Duplicate HTML ID: ' + attrs['id'])
            self.ids.add(attrs['id'])
        if tag == 'main': self.has_main = True
        if tag == 'html': self.has_lang = attrs.get('lang') == 'en'
        if tag == 'meta' and attrs.get('name') == 'viewport': self.has_viewport = True
        if tag == 'img' and 'alt' not in attrs: raise ValueError('Image missing alt attribute')
        if attrs.get('href', '').startswith('#'): self.anchors.append(attrs['href'][1:])
        if tag == 'script': raise ValueError('Foundation site must not execute scripts')
        if tag in ('img', 'link') and any(urlsplit(attrs.get(k, '')).scheme or urlsplit(attrs.get(k, '')).netloc for k in ('src', 'href')):
            raise ValueError('Foundation site assets must be local')


def check(root):
    authored = list(root.glob('*.md'))
    for folder in ('docs', 'brand', 'specs'):
        authored.extend((root / folder).rglob('*.md'))
    authored.append(root / 'docs/index.html')
    for source in authored: check_links(root, source)
    page = Page()
    page.feed((root / 'docs/index.html').read_text())
    if not (page.has_main and page.has_lang and page.has_viewport):
        raise ValueError('Site needs main landmark, language and viewport')
    if not set(page.anchors).issubset(page.ids): raise ValueError('Broken site anchor')
    for svg in (root / 'docs/assets').glob('*.svg'):
        tree = ET.parse(svg).getroot()
        ns = '{http://www.w3.org/2000/svg}'
        if tree.find(ns + 'title') is None or tree.find(ns + 'desc') is None:
            raise ValueError('SVG needs accessible title/description')
        if any('href' in key or node.tag.endswith('script') for node in tree.iter() for key in [*node.attrib, '']):
            raise ValueError('SVG external resources/scripts forbidden')
    css = root / 'docs/assets/site.css'
    for url in re.findall(r'url\([\"\']?([^\"\')]+)', css.read_text()):
        check_asset(root, css, url)
    for name in ('Bricolage-OFL.txt', 'IBMPlexSans-OFL.txt'):
        if 'SIL OPEN FONT LICENSE' not in (root / 'docs/assets/fonts' / name).read_text():
            raise ValueError('Missing font license: ' + name)
    state = json.loads((root / 'docs/feature-status.json').read_text())
    if state.get('schema_version') != 1: raise ValueError('Unknown feature status schema')
    for feature in state['features']:
        if feature['status'] not in ('planned', 'implemented', 'verified'):
            raise ValueError('Invalid feature status')
        for field in ('spec', 'evidence'):
            if feature.get(field): local_target(root, root / 'docs/feature-status.json', feature[field])
        if feature['status'] == 'verified' and not feature.get('evidence'):
            raise ValueError('Verified feature lacks evidence')
    print(f'Documentation checks passed ({len(authored)} authored documents).')


if __name__ == '__main__': check(ROOT)
