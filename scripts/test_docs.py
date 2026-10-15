# SPDX-License-Identifier: EUPL-1.2
import importlib.util
from pathlib import Path
import tempfile
import shutil
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
    def test_theme_script_is_the_only_runtime_script(self):
        checker = load('checker_theme', 'check-docs.py')
        allowed = '<script src="assets/theme.js" defer></script>'
        checker.Page().feed(allowed)
        forbidden = (
            '<script>alert(1)</script>',
            '<script src="assets/theme.js" defer>alert(1)</script>',
            '<script src="assets/theme.js"></script>',
            '<script src="https://example.org/theme.js" defer></script>',
            '<script src="//example.org/theme.js" defer></script>',
            '<script src="assets/other.js" defer></script>',
            '<script src="assets/theme.js" defer async></script>',
            '<script src="assets/theme.js" defer onload="alert(1)"></script>',
            '<button onclick="alert(1)">Theme</button>',
            allowed + allowed,
        )
        for html in forbidden:
            with self.subTest(html=html), self.assertRaises(ValueError):
                checker.Page().feed(html)

    def test_theme_structure_and_prominent_website_link(self):
        site = (ROOT / 'docs/index.html').read_text()
        self.assertIn('<label for="theme-select">Theme</label>', site)
        self.assertIn('id="theme-control" hidden', site)
        self.assertIn('<script src="assets/theme.js" defer></script>', site)
        for value in ('system', 'light', 'dark'):
            self.assertIn('value="' + value + '"', site)
        intro = (ROOT / 'README.md').read_text().split('SourceSteward is an early')[0]
        self.assertIn('[Project website](https://wstein.github.io/sourcesteward/)', intro)

    def test_packaging_accepts_only_named_theme_script(self):
        package = load('package_theme', 'package-pages.py')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            assets = root / 'docs/assets'
            assets.mkdir(parents=True)
            (root / 'docs/index.html').write_text('<html></html>')
            (assets / 'theme.js').write_text('// local theme')
            package.validate_assets(root)
            for path in (assets / 'other.js', assets / 'nested/theme.js'):
                path.parent.mkdir(exist_ok=True)
                path.write_text('// unexpected')
                with self.assertRaises(ValueError):
                    package.validate_assets(root)
                path.unlink()

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

    def test_raw_main_guidance_links_resolve_offline(self):
        checker = load('checker_raw', 'check-docs.py')
        prefix = 'https://raw.githubusercontent.com/wstein/sourcesteward/main/'
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'docs').mkdir()
            (root / 'docs/agent-guide.md').write_text('# Agent guide')
            source = root / 'llms.txt'
            source.write_text('[Guide](' + prefix + 'docs/agent-guide.md)')
            checker.check_links(root, source)
            for target in ('docs/missing.md', 'docs/', '../outside.md', '%2e%2e/outside.md'):
                with self.subTest(target=target):
                    source.write_text('[Missing](' + prefix + target + ')')
                    with self.assertRaises(ValueError): checker.check_links(root, source)
            outside = root.parent / (root.name + '-outside.md')
            outside.write_text('outside')
            try:
                (root / 'docs/escape.md').symlink_to(outside)
                source.write_text('[Escape](' + prefix + 'docs/escape.md)')
                with self.assertRaises(ValueError): checker.check_links(root, source)
            finally:
                outside.unlink()
            source.write_text('const guideURL = "' + prefix + 'missing.md"')
            with self.assertRaises(ValueError): checker.check_links(root, source)
            source.write_text('[Directory](docs/)')
            checker.check_links(root, source)
            source.write_text('[Unrelated](https://example.org/missing.md)')
            checker.check_links(root, source)

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
            self.assertTrue((destination / 'docs/roadmap/index.html').exists())
            self.assertTrue((destination / 'reference/stw-version/index.html').exists())
            self.assertEqual((destination / 'revision.txt').read_text(), 'a' * 40 + '\n')
            with self.assertRaises(ValueError): package.package(ROOT, destination, 'b' * 40)

    def test_schema_packaging_is_verbatim_and_rejects_unexpected_sources(self):
        package = load('package_schemas', 'package-pages.py')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(ROOT / 'docs/assets', root / 'docs/assets')
            shutil.copyfile(ROOT / 'docs/index.html', root / 'docs/index.html')
            shutil.copytree(ROOT / 'schemas', root / 'schemas')
            destination = root / 'output'
            package.copy_schemas(root, destination)
            for name in ('version.schema.json', 'inspect.schema.json'):
                self.assertEqual((destination / 'schemas/v1' / name).read_bytes(),
                                 (ROOT / 'schemas/v1' / name).read_bytes())
            self.assertEqual((destination / 'schemas/v2/inspect.schema.json').read_bytes(),
                             (ROOT / 'schemas/v2/inspect.schema.json').read_bytes())
            shutil.rmtree(destination)
            extra = root / 'schemas/v1/private.json'
            extra.write_text('{}')
            with self.assertRaises(ValueError): package.copy_schemas(root, destination)
            self.assertFalse(destination.exists())
            extra.unlink()
            missing = root / 'schemas/v1/inspect.schema.json'
            saved_bytes = missing.read_bytes()
            missing.unlink()
            with self.assertRaises(ValueError): package.copy_schemas(root, destination)
            self.assertFalse(destination.exists())
            missing.write_bytes(saved_bytes)
            for source in (root / 'schemas/v1/version.schema.json', root / 'schemas/v1', root / 'schemas'):
                saved = source.with_name(source.name + '-saved')
                source.rename(saved)
                source.symlink_to(saved, target_is_directory=saved.is_dir())
                with self.assertRaises(ValueError): package.copy_schemas(root, destination)
                self.assertFalse(destination.exists())
                source.unlink()
                saved.rename(source)

    def test_packaging_rejects_symlinks_and_invalid_revision(self):
        package = load('package', 'package-pages.py')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'docs/assets').mkdir(parents=True)
            (root / 'docs/index.html').write_text('<html></html>')
            (root / 'docs/assets/leak.txt').symlink_to(ROOT / 'go.mod')
            with self.assertRaises(ValueError): package.validate_assets(root)
            self.assertFalse((root / 'output').exists())
            with self.assertRaises(ValueError): package.package(ROOT, root / 'output', 'invalid')

if __name__ == '__main__': unittest.main()
