# SPDX-License-Identifier: EUPL-1.2
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('pages_hugo', ROOT / 'scripts/package-pages.py')
pages = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pages)

class HugoTests(unittest.TestCase):
    def test_public_guide_allowlist_and_link_mapping(self):
        self.assertEqual(pages.GUIDES['docs/development.md'][0], 'development')
        self.assertNotIn('docs/harness.md', pages.GUIDES)
        source = ROOT / 'docs/development.md'
        transformed = pages.guide_markdown(ROOT, source, '[Backend](backends.md) [Harness](harness.md)')
        self.assertIn('/sourcesteward/docs/backends/', transformed)
        self.assertIn('https://github.com/wstein/sourcesteward/blob/main/docs/harness.md', transformed)

    def test_shortcodes_and_unsafe_links_rejected(self):
        for body in ('{{< youtube abc >}}', '{{% include "private" %}}', '[Escape](../../outside.md)', '[Bad](javascript:alert)'):
            with self.subTest(body=body), self.assertRaises(ValueError):
                pages.guide_markdown(ROOT, ROOT / 'docs/development.md', body)

    def test_schema_copy_is_byte_exact_and_rejects_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / 'schemas/v1'
            source.mkdir(parents=True)
            raw = b'{"$schema":"https://json-schema.org/draft/2020-12/schema"}\n'
            (source / 'version.schema.json').write_bytes(raw)
            (source / 'inspect.schema.json').write_bytes(raw)
            destination = root / 'public'
            destination.mkdir()
            pages.copy_schemas(root,destination,required=True)
            self.assertEqual((destination / 'schemas/v1/version.schema.json').read_bytes(),raw)
            (source / 'inspect.schema.json').unlink()
            with self.assertRaises(ValueError): pages.copy_schemas(root,root / 'bad',required=True)
            (source / 'inspect.schema.json').symlink_to(source / 'version.schema.json')
            with self.assertRaises(ValueError): pages.copy_schemas(root,root / 'bad',required=True)

    def test_hugo_security_and_project_base(self):
        config = (ROOT / 'site/hugo.toml').read_text()
        for required in ('baseURL = "https://wstein.github.io/sourcesteward/"', 'unsafe = false', 'allow = []', 'urls = []', 'enableInlineShortcodes = false'):
            self.assertIn(required,config)

    def test_generated_artifact_checks_links_privacy_and_runtime_assets(self):
        spec = importlib.util.spec_from_file_location('built_site',ROOT / 'scripts/check-site.py')
        check_site = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(check_site)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'revision.txt').write_text('a'*40+'\n')
            (root / 'assets').mkdir()
            (root / 'assets/theme.js').write_text('// theme')
            good = '<html lang="en"><meta name="viewport"><script src="/sourcesteward/assets/theme.js" defer></script><main id="main">OK</main></html>'
            (root / 'index.html').write_text(good)
            check_site.validate(root,'a'*40,source_root=root)
            for extra in ('<a href="/docs/">Lost base</a>','<a href="/sourcesteward/missing/">Broken</a>','<img alt="" src="https://example.org/a.png">'):
                (root / 'index.html').write_text(good + extra)
                with self.subTest(extra=extra), self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=root)
            (root / 'index.html').write_text(good.replace('<script src="/sourcesteward/assets/theme.js" defer></script>',''))
            with self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=root)
            (root / 'index.html').write_text(good)
            (root / 'AGENTS.md').write_text('private')
            with self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=root)
