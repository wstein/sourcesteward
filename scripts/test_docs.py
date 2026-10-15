# SPDX-License-Identifier: EUPL-1.2
import importlib.util
from pathlib import Path
import tempfile
import struct
import hashlib
import xml.etree.ElementTree as ET
import unittest

ROOT = Path(__file__).resolve().parents[1]
def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

class DocumentationTests(unittest.TestCase):
    def test_wide_tie_assets_are_flat_and_self_contained(self):
        allowed = {'#0b2545', '#f05032', '#ff8066', '#ffffff', 'none'}
        ns = '{http://www.w3.org/2000/svg}'
        for asset in sorted((ROOT / 'docs/assets').glob('*.svg')):
            root = ET.parse(asset).getroot()
            self.assertIsNotNone(root.find(ns + 'title'))
            self.assertIsNotNone(root.find(ns + 'desc'))
            if asset.name in ('logo.svg', 'logo-mark.svg', 'favicon.svg'):
                self.assertEqual(len(root.findall('.//' + ns + 'circle')), 3)
            for node in root.iter():
                self.assertNotIn(node.tag.removeprefix(ns), ('image', 'linearGradient', 'radialGradient', 'filter', 'script', 'text'))
                self.assertFalse(any('href' in key for key in node.attrib))
                for key in ('fill', 'stroke'):
                    if key in node.attrib: self.assertIn(node.attrib[key], allowed)
        site = (ROOT / 'docs/index.html').read_text()
        self.assertIn('href="assets/favicon.svg"', site)
        self.assertNotIn('#5eead4', (ROOT / 'docs/assets/site.css').read_text())

    def test_brand_raster_dimensions_and_hashes(self):
        sizes = {'logo.png':(256,256), 'banner.png':(1000,150), 'social-preview.png':(1280,640),
                 'apple-touch-icon.png':(180,180), 'android-chrome-192.png':(192,192), 'android-chrome-512.png':(512,512)}
        sizes.update({f'favicon-{n}.png':(n,n) for n in (16,24,32,48,64)})
        for name, size in sizes.items():
            raw = (ROOT / 'docs/assets' / name).read_bytes()
            self.assertEqual(raw[:8], b'\x89PNG\r\n\x1a\n')
            self.assertEqual(struct.unpack('>II', raw[16:24]), size)
        self.assertEqual((ROOT / 'docs/assets/apple-touch-icon.png').read_bytes()[25], 2)
        icon = (ROOT / 'docs/assets/favicon.ico').read_bytes()
        self.assertEqual(struct.unpack('<HHH',icon[:6]), (0,1,4))
        for line in (ROOT / 'brand/SHA256SUMS').read_text().splitlines():
            expected, name = line.split('  ',1)
            self.assertEqual(hashlib.sha256((ROOT / name).read_bytes()).hexdigest(), expected, name)

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
            self.assertTrue((destination / 'assets/favicon.ico').is_file())
            self.assertTrue((destination / 'assets/social-preview.png').is_file())
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
