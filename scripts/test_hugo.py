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
        self.assertEqual(pages.GUIDES['docs/json-contracts.md'][0], 'json-contracts')
        self.assertEqual(pages.GUIDES['docs/tidy.md'][0], 'tidy')
        self.assertEqual(pages.GUIDES['docs/commands.md'][0], 'commands')
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
            (root / 'schemas/v2').mkdir()
            (root / 'schemas/v2/inspect.schema.json').write_text('{}')
            (root / 'schemas/v2/tidy.schema.json').write_text('{}')
            raw = b'{"$schema":"https://json-schema.org/draft/2020-12/schema"}\n'
            (source / 'version.schema.json').write_bytes(raw)
            (source / 'inspect.schema.json').write_bytes(raw)
            (source / 'inventory.schema.json').write_bytes(raw)
            (source / 'workspace.schema.json').write_bytes(raw)
            (source / 'tidy.schema.json').write_bytes(raw)
            (source / 'hosting.schema.json').write_bytes(raw)
            (source / 'pins.schema.json').write_bytes(raw)
            (source / 'repos.schema.json').write_bytes(raw)
            (source / 'release-config.schema.json').write_bytes(raw)
            (source / 'release-check.schema.json').write_bytes(raw)
            destination = root / 'public'
            destination.mkdir()
            pages.copy_schemas(root,destination,required=True)
            self.assertEqual((destination / 'schemas/v1/version.schema.json').read_bytes(),raw)
            self.assertEqual((destination / 'schemas/v2/inspect.schema.json').read_bytes(),b'{}')
            self.assertEqual(pages.guide_markdown(ROOT, ROOT / 'docs/json-contracts.md', '[JJ](../schemas/v2/inspect.schema.json)'), '[JJ](/sourcesteward/schemas/v2/inspect.schema.json)')
            (root / 'schemas/private.json').write_text('{}')
            with self.assertRaises(ValueError): pages.copy_schemas(root,root / 'bad',required=True)
            (root / 'schemas/private.json').unlink()
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
            source_root = root / 'source'
            for version, names in {'v1':('version.schema.json','inspect.schema.json','inventory.schema.json', 'workspace.schema.json', 'tidy.schema.json', 'hosting.schema.json', 'pins.schema.json', 'repos.schema.json', 'release-config.schema.json', 'release-check.schema.json'),'v2':('inspect.schema.json', 'tidy.schema.json')}.items():
                for name in names:
                    source = source_root / 'schemas' / version / name
                    source.parent.mkdir(parents=True,exist_ok=True)
                    source.write_text('{}')
                    target = root / 'schemas' / version / name
                    target.parent.mkdir(parents=True,exist_ok=True)
                    target.write_text('{}')
            (root / 'assets').mkdir()
            (root / 'assets/theme.js').write_text('// theme')
            good = '<html lang="en"><meta name="viewport"><script src="/sourcesteward/assets/theme.js" defer></script><main id="main">OK</main></html>'
            (root / 'index.html').write_text(good)
            check_site.validate(root,'a'*40,source_root=source_root)
            for extra in ('<a href="/docs/">Lost base</a>','<a href="/sourcesteward/missing/">Broken</a>','<img alt="" src="https://example.org/a.png">'):
                (root / 'index.html').write_text(good + extra)
                with self.subTest(extra=extra), self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=source_root)
            (root / 'index.html').write_text(good.replace('<script src="/sourcesteward/assets/theme.js" defer></script>',''))
            with self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=source_root)
            (root / 'index.html').write_text(good)
            schema = root / 'schemas/v2/inspect.schema.json'
            schema.write_text('{"changed":true}')
            with self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=source_root)
            schema.write_text('{}')
            schema.unlink()
            schema.symlink_to(source_root / 'schemas/v2/inspect.schema.json')
            with self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=source_root)
            schema.unlink(); schema.write_text('{}')
            extra = root / 'schemas/v2/version.schema.json'
            extra.write_text('{}')
            with self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=source_root)
            extra.unlink()
            (root / 'AGENTS.md').write_text('private')
            with self.assertRaises(ValueError): check_site.validate(root,'a'*40,source_root=source_root)

    def test_all_public_guides_and_llms_are_mandatory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'docs').mkdir()
            for name in pages.GUIDES: (root / name).write_text('# Public guide')
            (root / 'llms.txt').write_text('# Public hints')
            pages.validate_public_sources(root)
            (root / 'docs/agent-guide.md').unlink()
            with self.assertRaises(ValueError): pages.validate_public_sources(root)
            (root / 'docs/agent-guide.md').write_text('# Public guide')
            (root / 'llms.txt').unlink()
            with self.assertRaises(ValueError): pages.validate_public_sources(root)

    def test_all_schema_versions_are_required_and_strict(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for version, names in {'v1':('version.schema.json','inspect.schema.json','inventory.schema.json', 'workspace.schema.json', 'tidy.schema.json', 'hosting.schema.json', 'pins.schema.json'),'v2':('inspect.schema.json', 'tidy.schema.json')}.items():
                for name in names:
                    path = root / 'schemas' / version / name
                    path.parent.mkdir(parents=True,exist_ok=True)
                    path.write_text('{}')
            paths = ('schemas/v2/inspect.schema.json', 'schemas/v2', 'schemas')
            for name in paths:
                source = root / name
                saved = source.with_name(source.name + '-saved')
                source.rename(saved)
                source.symlink_to(saved,target_is_directory=saved.is_dir())
                with self.subTest(link=name), self.assertRaises(ValueError): pages.copy_schemas(root,root / 'out')
                source.unlink();saved.rename(source)
            for name in ('schemas/v2/version.schema.json','schemas/v3/private.json','schemas/private.json'):
                path = root / name;path.parent.mkdir(parents=True,exist_ok=True);path.write_text('{}')
                with self.subTest(extra=name), self.assertRaises(ValueError): pages.copy_schemas(root,root / 'out')
                path.unlink()
                if path.parent.name == 'v3': path.parent.rmdir()
            (root / 'schemas/v2/inspect.schema.json').unlink()
            with self.assertRaises(ValueError): pages.copy_schemas(root,root / 'out')
