# SPDX-License-Identifier: EUPL-1.2
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

class DocumentationTests(unittest.TestCase):
    def test_architecture_rejects_direct_effects(self):
        architecture = load('architecture', 'check-architecture.py')
        architecture.validate(['github.com/wstein/sourcesteward/internal/cli fmt context github.com/spf13/cobra'])
        for dependency in ('os/exec', 'net/http', 'github.com/wstein/sourcesteward/internal/backend'):
            with self.assertRaises(ValueError):
                architecture.validate(['github.com/wstein/sourcesteward/internal/cli ' + dependency])

    def test_runtime_assets_reject_external_and_protocol_relative_urls(self):
        checker = load('checker_assets', 'check-docs.py')
        for target in ('//example.org/a.css', 'https://example.org/a.css'):
            for tag in ('<img alt="" src="'+target+'">', '<link href="'+target+'">'):
                with self.assertRaises(ValueError): checker.Page().feed(tag)
            with self.assertRaises(ValueError): checker.check_asset(ROOT, ROOT / 'docs/assets/site.css', target)

    def test_broken_local_links_fail(self):
        checker = load('checker', 'check-docs.py')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'README.md').write_text('[Missing](docs/missing.md)')
            with self.assertRaises(ValueError): checker.check_links(root, root / 'README.md')
            (root / 'README.md').write_text('[Web](https://example.org/)')
            checker.check_links(root, root / 'README.md')

    def test_packaging_excludes_source_and_binds_revision(self):
        package = load('package', 'package-pages.py')
        with tempfile.TemporaryDirectory() as directory:
            destination = Path(directory) / 'site'
            package.package(ROOT, destination, 'a' * 40)
            self.assertTrue((destination / 'index.html').is_file())
            self.assertTrue((destination / 'assets/logo.svg').is_file())
            self.assertFalse((destination / 'roadmap.md').exists())
            self.assertEqual((destination / 'revision.txt').read_text(), 'a' * 40 + '\n')
            with self.assertRaises(ValueError): package.package(ROOT, destination, 'b' * 40)

    def test_packaging_rejects_symlinks_and_invalid_revision(self):
        package = load('package', 'package-pages.py')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'docs/assets').mkdir(parents=True)
            (root / 'docs/index.html').write_text('<html></html>')
            (root / 'docs/assets/leak.txt').symlink_to(ROOT / 'go.mod')
            with self.assertRaises(ValueError): package.package(root, root / 'output', 'a' * 40)
            self.assertFalse((root / 'output').exists())
            with self.assertRaises(ValueError): package.package(ROOT, root / 'output', 'invalid')

if __name__ == '__main__': unittest.main()
