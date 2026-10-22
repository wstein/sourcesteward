#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Stage an explicit public allowlist and build one checked Hugo artifact."""
from pathlib import Path
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from urllib.parse import unquote, urlsplit

BASE = '/sourcesteward/'
GUIDES = {
    'docs/commands.md': ('commands', 'Implemented commands and examples'),
    'docs/tidy.md': ('tidy', 'Evidence-based tidy'),
    'docs/workspaces.md': ('workspaces', 'Task workspaces'),
    'docs/hosting.md': ('hosting', 'GitHub inspection and gh jj'),
    'docs/inventory.md': ('inventory', 'Inventory'),
    'docs/development.md': ('development', 'Development'),
    'docs/backends.md': ('backends', 'Backend readiness'),
    'docs/roadmap.md': ('roadmap', 'Roadmap'),
    'docs/agent-guide.md': ('agents', 'Agent guide'),
    'docs/json-contracts.md': ('json-contracts', 'JSON contracts'),
}
SCHEMAS = {'v1': ('version.schema.json', 'inspect.schema.json', 'inventory.schema.json', 'workspace.schema.json', 'tidy.schema.json', 'hosting.schema.json', 'pins.schema.json', 'repos.schema.json'), 'v2': ('inspect.schema.json', 'tidy.schema.json', 'hosting-pr.schema.json', 'hosting-prs.schema.json')}

# Schemas of features whose commands are still hidden: they exist in the source tree and are
# validated, but are not copied to the site. The documentation work item of the feature removes
# them from this set (and adds them to SCHEMAS) when it publishes the contracts.
UNPUBLISHED_SCHEMAS = {'v1/restack-plan.schema.json', 'v1/release-config.schema.json', 'v1/release-check.schema.json', 'v1/release-record.schema.json', 'v1/release-build-manifest.schema.json', 'v1/release-oci-result.schema.json', 'v1/release-plan.schema.json', 'v1/release-verify.schema.json', 'v1/release-record-result.schema.json', 'v1/release-build.schema.json', 'v1/release-dispatch.schema.json', 'v1/sync.schema.json', 'v1/status.schema.json', 'v1/pulls.schema.json', 'v1/issues.schema.json', 'v1/watch-event.schema.json', 'v3/tidy.schema.json', 'v3/tidy-report.schema.json', 'v3/tidy-decisions.schema.json', 'v3/tidy-restore.schema.json', 'v3/tidy-history.schema.json'}

SCHEMA_PATHS = {version + '/' + name for version, names in SCHEMAS.items() for name in names}


def validate_assets(root):
    index, assets = root / 'docs/index.html', root / 'docs/assets'
    if (root / 'docs').is_symlink() or index.is_symlink() or assets.is_symlink() or not index.is_file() or not assets.is_dir():
        raise ValueError('Regular static site sources required')
    for path in assets.rglob('*'):
        if path.is_symlink() or (path.is_file() and path.suffix not in ('.svg', '.png', '.ico', '.css', '.ttf', '.woff2', '.txt') and path != assets / 'theme.js'):
            raise ValueError('Unexpected or linked site asset: ' + str(path))
    return index, assets


def validate_public_sources(root):
    for name in (*GUIDES,'llms.txt'):
        source = root / name
        if source.is_symlink() or not source.is_file():
            raise ValueError('Regular public source required: ' + name)


def guide_markdown(root, source, text):
    if '{{<' in text or '{{%' in text: raise ValueError('Hugo source shortcodes forbidden: ' + str(source))
    def rewrite(match):
        label, target = match.groups()
        url = urlsplit(target)
        if url.scheme:
            if url.scheme not in ('http','https','mailto'): raise ValueError('Unsafe guide URL: ' + target)
            return match.group(0)
        if url.netloc: raise ValueError('Protocol-relative guide URL forbidden')
        if not url.path: return match.group(0)
        path = (source.parent / unquote(url.path)).resolve()
        if not path.is_relative_to(root.resolve()): raise ValueError('Guide link escapes source tree: ' + target)
        relative = path.relative_to(root.resolve()).as_posix()
        if relative in GUIDES and path.is_file():
            destination = BASE + 'docs/' + GUIDES[relative][0] + '/'
        elif relative.removeprefix('schemas/') in SCHEMA_PATHS and relative.startswith('schemas/'):
            destination = BASE + relative
        else:
            destination = 'https://github.com/wstein/sourcesteward/blob/main/' + relative
        if url.fragment: destination += '#' + url.fragment
        return '[' + label + '](' + destination + ')'
    # Authored guides use ordinary inline Markdown links, not reference definitions.
    return re.sub(r'\[([^\]]*)\]\(([^\s)]+)\)',rewrite,text)


def schema_sources(root):
    schema_root = root / 'schemas'
    if schema_root.is_symlink() or not schema_root.is_dir():
        raise ValueError('Regular schema directory required')
    expected = set(SCHEMAS) | SCHEMA_PATHS
    # Only explicitly listed unpublished sources may introduce a hidden directory.
    unpublished_dirs = {Path(name).parent.as_posix() for name in UNPUBLISHED_SCHEMAS} - set(SCHEMAS)
    actual = set()
    for path in schema_root.rglob('*'):
        relative = path.relative_to(schema_root).as_posix()
        if relative in UNPUBLISHED_SCHEMAS and path.is_file() and not path.is_symlink():
            json.loads(path.read_text())
            continue
        if relative in unpublished_dirs and path.is_dir() and not path.is_symlink():
            continue
        if path.is_symlink() or relative not in expected or (relative in SCHEMAS and not path.is_dir()) or (relative in SCHEMA_PATHS and not path.is_file()):
            raise ValueError('Unexpected or linked schema source: ' + str(path))
        actual.add(relative)
    if actual != expected: raise ValueError('All published schema contracts are required')
    paths = [schema_root / name for name in sorted(SCHEMA_PATHS)]
    for path in paths: json.loads(path.read_text())
    return paths


def copy_schemas(root, destination, required=True):
    if not (root / 'schemas').exists() and not (root / 'schemas').is_symlink() and not required:
        return
    paths = schema_sources(root)
    for path in paths:
        target = destination / 'schemas' / path.relative_to(root / 'schemas')
        target.parent.mkdir(parents=True,exist_ok=True)
        shutil.copyfile(path,target)


def write_content(path,title,body):
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text('+++\ntitle = ' + json.dumps(title) + '\n+++\n\n' + body)


def package(root, destination, revision, hugo=None, require_schemas=True):
    root, destination = root.resolve(), destination.absolute()
    if not re.fullmatch(r'[0-9a-f]{40}',revision): raise ValueError('Full source SHA required')
    if destination.exists() or destination.is_symlink(): raise ValueError('Destination must not exist')
    index, assets = validate_assets(root)
    validate_public_sources(root)
    hugo = str(hugo or os.environ.get('HUGO') or root / '.tools/hugo')
    pin = json.loads((root / 'scripts/hugo-release.json').read_text())['version']
    version = subprocess.check_output([hugo,'version'],text=True)
    if not re.match(r'^hugo v' + re.escape(pin) + r'(?:-[0-9a-f]+)? ',version): raise ValueError('Hugo ' + pin + ' required; run scripts/install-hugo.py')
    with tempfile.TemporaryDirectory(prefix='stw-pages-') as directory:
        stage = Path(directory)
        for name in ('hugo.toml','layouts'):
            source = root / 'site' / name
            if source.is_symlink() or any(p.is_symlink() for p in source.rglob('*')): raise ValueError('Linked Hugo source forbidden')
            if source.is_dir(): shutil.copytree(source,stage / name)
            else: shutil.copyfile(source,stage / name)
        shutil.copytree(assets,stage / 'static/assets')
        # Home remains authored HTML; only exact local URL attributes are rebased.
        home = re.sub(r'(href|src)="assets/',r'\1="' + BASE + 'assets/',index.read_text())
        home = home.replace('href="docs/"','href="' + BASE + 'docs/"')
        for source, (slug, _) in GUIDES.items():
            home = home.replace('href="' + Path(source).name + '"', 'href="' + BASE + 'docs/' + slug + '/"')
        # A trusted repo-owned layout, never source Markdown as a template.
        (stage / 'layouts/index.html').write_text(home)
        content = stage / 'content'
        write_content(content / '_index.md','SourceSteward','')
        write_content(content / 'docs/_index.md','Documentation','Current public guides. Repository operations remain limited to the implemented command contract.\n')
        for name,(slug,title) in GUIDES.items():
            source = root / name
            body = guide_markdown(root,source,source.read_text())
            body = re.sub(r'^# [^\n]*\n','',body,count=1)
            write_content(content / 'docs' / slug / 'index.md',title,body)
        reference = json.loads(subprocess.check_output(['go','run','-buildvcs=false','./cmd/stw-docs'],cwd=root,text=True))
        write_content(content / 'reference/_index.md','CLI reference','Generated from the current Cobra command tree. No repository command is executed during generation.\n')
        for command in reference:
            slug = command['path'].replace(' ','-')
            body = command['long'] + '\n\n## Usage\n\n```text\n' + command['usage'] + '\n```\n\n## Flags\n\n```text\n' + command['flags'] + '```\n'
            write_content(content / 'reference' / slug / 'index.md',command['path'],body)
        copy_schemas(root,stage / 'static',required=require_schemas)
        llms = root / 'llms.txt'
        shutil.copyfile(llms,stage / 'static/llms.txt')
        subprocess.run([hugo,'--source',str(stage),'--destination',str(stage / 'public'),'--noBuildLock'],check=True,env={**{key:value for key,value in os.environ.items() if not key.startswith('HUGO_')},'HUGO_ENVIRONMENT':'production'})
        destination.parent.mkdir(parents=True,exist_ok=True)
        shutil.copytree(stage / 'public',destination)
    (destination / 'revision.txt').write_text(revision + '\n')
    (destination / '.nojekyll').touch()


if __name__ == '__main__':
    if len(sys.argv) != 3: raise SystemExit('usage: package-pages.py DESTINATION FULL_SOURCE_SHA')
    package(Path(__file__).resolve().parents[1],Path(sys.argv[1]),sys.argv[2])
