#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Package only the static website and its exact source revision."""
from pathlib import Path
import re
import shutil
import sys


def package(root, destination, revision):
    root = root.resolve()
    if not re.fullmatch(r'[0-9a-f]{40}', revision): raise ValueError('Full source SHA required')
    if destination.exists() or destination.is_symlink(): raise ValueError('Destination must not exist')
    index, assets = root / 'docs/index.html', root / 'docs/assets'
    if index.is_symlink() or assets.is_symlink() or not index.is_file() or not assets.is_dir():
        raise ValueError('Regular static site sources required')
    for path in assets.rglob('*'):
        if path.is_symlink() or (path.is_file() and path.suffix not in ('.svg', '.png', '.ico', '.css', '.ttf', '.woff2', '.txt') and path != assets / 'theme.js'):
            raise ValueError('Unexpected or linked site asset: ' + str(path))
    schemas = root / 'schemas'
    version = schemas / 'v1'
    schema_names = ('version.schema.json', 'inspect.schema.json')
    if schemas.is_symlink() or version.is_symlink() or not schemas.is_dir() or not version.is_dir():
        raise ValueError('Regular versioned schema directory required')
    expected = {'v1'} | {'v1/' + name for name in schema_names}
    actual = set()
    for path in schemas.rglob('*'):
        relative = path.relative_to(schemas).as_posix()
        if path.is_symlink() or relative not in expected or (relative != 'v1' and not path.is_file()):
            raise ValueError('Unexpected or linked schema source: ' + str(path))
        actual.add(relative)
    if actual != expected:
        raise ValueError('Both schema 1 contracts are required')
    destination.mkdir(parents=True)
    shutil.copyfile(index, destination / 'index.html')
    shutil.copytree(assets, destination / 'assets')
    (destination / 'schemas/v1').mkdir(parents=True)
    for name in schema_names:
        shutil.copyfile(version / name, destination / 'schemas/v1' / name)
    (destination / 'revision.txt').write_text(revision + '\n')
    (destination / '.nojekyll').touch()


if __name__ == '__main__':
    if len(sys.argv) != 3: raise SystemExit('usage: package-pages.py DESTINATION FULL_SOURCE_SHA')
    package(Path(__file__).resolve().parents[1], Path(sys.argv[1]), sys.argv[2])
