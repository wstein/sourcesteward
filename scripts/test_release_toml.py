#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Differential test of the strict TOML profile parser (Spec 026, contracts/config.md).

Python tomllib is the reference TOML parser: every file the Go profile parser accepts must parse
in tomllib to the same data, and the constructs tomllib accepts but the profile forbids (dotted
keys, inline tables, split tables, interleaved arrays of tables) must be rejected by the profile.
The Go side is the internal/release test TestTOMLCorpusDump; nothing is built or published.
"""
import json
import os
import random
import subprocess
import tempfile
import tomllib
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
EXAMPLES = sorted((ROOT / 'specs' / '026-release-process' / 'examples').glob('*.release.toml'))

# Constructs that tomllib accepts and the profile must reject (measured, research.md). The split
# table is the sub-table form; a plain repeated [a] is rejected by tomllib too (see BOTH_REJECT).
TOMLLIB_ACCEPTS_PROFILE_REJECTS = {
    'dotted-key': 'a.b = 1\n',
    'dotted-table': '[a.b]\nx = 1\n',
    'inline-table': 'a = { b = 1 }\n',
    'interleaved-array': '[[t]]\nx = 1\n[u]\ny = 1\n[[t]]\nx = 2\n',
    'table-then-subtable': '[x]\n[y]\n[x.z]\n',
    'inline-table-in-array': 'a = [{ b = 1 }]\n',
}

# Constructs both parsers reject.
BOTH_REJECT = {
    'duplicate-key': 'a = 1\na = 2\n',
    'repeated-table': '[a]\nx = 1\n[b]\ny = 1\n[a]\nz = 1\n',
    'unterminated-string': 'a = "x\n',
    'bad-escape': 'a = "\\q"\n',
    'unterminated-array': 'a = ["x"\n',
}

# Valid TOML that is outside the profile; tomllib parses these, the profile must not.
TOMLLIB_ACCEPTS_OUTSIDE_PROFILE = {
    'float': 'a = 1.5\n',
    'date': 'a = 1979-05-27\n',
    'multiline-string': 'a = """x"""\n',
    'hex': 'a = 0xFF\n',
    'plus-integer': 'a = +5\n',
    'underscore': 'a = 1_000\n',
    'array-of-integers': 'a = [1, 2]\n',
    'nested-array': 'a = [["x"]]\n',
    'mixed-array': 'a = ["x", 1]\n',
    'quoted-key': '"a" = 1\n',
}

STRINGS = ['', 'plain', 'with space', 'tab\there', 'quote"inside', 'back\\slash', 'new\nline', 'caf\u00e9',
           '\U0001F600', 'ctrl\u0001', '{version}', 'a=b', '#not-a-comment', "it's", 'dist/release/x_{os}.tar.gz']
KEYS = ['name', 'id', 'argv', 'files', 'x_y', 'a-b', 'A1', 'timeout_seconds', 'k']


def quote(rng, text):
    if rng.random() < 0.3 and '\n' not in text and '\\' not in text and "'" not in text and not any(ord(c) < 32 or ord(c) == 127 for c in text):
        return "'" + text + "'"
    out = []
    for c in text:
        if c == '"':
            out.append('\\"')
        elif c == '\\':
            out.append('\\\\')
        elif c == '\n':
            out.append('\\n')
        elif c == '\t':
            out.append('\\t')
        elif ord(c) < 32 or ord(c) == 127:
            out.append('\\u%04x' % ord(c))
        elif rng.random() < 0.1:
            out.append('\\u%04x' % ord(c) if ord(c) < 0x10000 else '\\U%08x' % ord(c))
        else:
            out.append(c)
    return '"' + ''.join(out) + '"'


def value(rng):
    kind = rng.choice(['string', 'string', 'int', 'bool', 'array'])
    if kind == 'string':
        return quote(rng, rng.choice(STRINGS))
    if kind == 'int':
        return str(rng.choice([0, -0, 1, -1, 42, 7200, 9223372036854775807, -9223372036854775808]))
    if kind == 'bool':
        return rng.choice(['true', 'false'])
    items = [quote(rng, rng.choice(STRINGS)) for _ in range(rng.randint(0, 4))]
    if rng.random() < 0.5:
        body = ''.join('\n  %s,%s' % (i, ' # note' if rng.random() < 0.3 else '') for i in items)
        return '[' + body + ('\n' if items else '') + ']'
    return '[' + ', '.join(items) + (',' if items and rng.random() < 0.3 else '') + ']'


def table_body(rng):
    lines = []
    for key in rng.sample(KEYS, rng.randint(0, 5)):
        lines.append('%s%s%s=%s%s%s' % (rng.choice(['', ' ']), key, rng.choice([' ', '']), rng.choice([' ', '']), value(rng), rng.choice(['', ' # c'])))
    return lines


def generated_document(rng):
    lines = ['# generated']
    lines += table_body(rng)
    tables = rng.sample(['project', 'version', 'outputs', 'allowlist', 'ci'], rng.randint(0, 4))
    arrays = rng.sample(['build', 'target'], rng.randint(0, 2))
    order = [('table', t) for t in tables] + [('array', a) for a in arrays]
    rng.shuffle(order)
    for kind, name in order:
        for _ in range(1 if kind == 'table' else rng.randint(1, 3)):
            lines.append(rng.choice(['', '# section']))
            lines.append('[%s]' % name if kind == 'table' else '[[%s]]' % name)
            lines += table_body(rng)
    text = '\n'.join(lines) + '\n'
    return text.replace('\n', '\r\n') if rng.random() < 0.2 else text


def same(a, b):
    """Equality that does not treat True as 1."""
    if type(a) is not type(b):
        return False
    if isinstance(a, dict):
        return a.keys() == b.keys() and all(same(a[k], b[k]) for k in a)
    if isinstance(a, list):
        return len(a) == len(b) and all(same(x, y) for x, y in zip(a, b))
    return a == b


def go_verdicts(corpus):
    with tempfile.TemporaryDirectory() as tmp:
        for name, text in corpus.items():
            (Path(tmp) / (name + '.toml')).write_bytes(text if isinstance(text, bytes) else text.encode('utf-8'))
        env = dict(os.environ, STW_TOML_CORPUS=tmp)
        result = subprocess.run(['go', 'test', './internal/release', '-run', '^TestTOMLCorpusDump$', '-count=1'], cwd=ROOT, env=env, text=True, capture_output=True)
        if result.returncode != 0:
            raise AssertionError(result.stdout + result.stderr)
        return {name: json.loads((Path(tmp) / (name + '.json')).read_text()) for name in corpus}


def reference(text):
    try:
        return tomllib.loads(text if isinstance(text, str) else text.decode('utf-8'))
    except (tomllib.TOMLDecodeError, UnicodeDecodeError):
        return None


class ReleaseTOMLProfileTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        rng = random.Random(26)
        cls.corpus = {}
        for path in EXAMPLES:
            cls.corpus['example-' + path.stem] = path.read_bytes()
        for i in range(80):
            cls.corpus['generated-%03d' % i] = generated_document(rng)
        for group in (TOMLLIB_ACCEPTS_PROFILE_REJECTS, BOTH_REJECT, TOMLLIB_ACCEPTS_OUTSIDE_PROFILE):
            cls.corpus.update(group)
        cls.verdicts = go_verdicts(cls.corpus)

    def test_six_examples_are_accepted_with_identical_data(self):
        self.assertEqual(len(EXAMPLES), 6)
        for path in EXAMPLES:
            name = 'example-' + path.stem
            self.assertTrue(self.verdicts[name]['accepted'], name)
            self.assertTrue(same(self.verdicts[name]['data'], reference(self.corpus[name])), name)

    def test_generated_profile_documents_are_accepted_with_identical_data(self):
        for name in (n for n in self.corpus if n.startswith('generated-')):
            verdict = self.verdicts[name]
            self.assertTrue(verdict['accepted'], name + repr(self.corpus[name]))
            self.assertTrue(same(verdict['data'], reference(self.corpus[name])), name)

    def test_everything_the_profile_accepts_parses_in_tomllib_to_the_same_data(self):
        for name, verdict in self.verdicts.items():
            if verdict['accepted']:
                self.assertTrue(same(verdict['data'], reference(self.corpus[name])), name)

    def test_the_constructs_tomllib_accepts_are_rejected_by_the_profile(self):
        self.assertGreaterEqual(len(TOMLLIB_ACCEPTS_PROFILE_REJECTS), 3)
        for name, text in TOMLLIB_ACCEPTS_PROFILE_REJECTS.items():
            self.assertIsNotNone(reference(text), name + ' is no longer accepted by tomllib; update the measurement')
            self.assertFalse(self.verdicts[name]['accepted'], name)

    def test_valid_toml_outside_the_profile_is_rejected(self):
        for name, text in TOMLLIB_ACCEPTS_OUTSIDE_PROFILE.items():
            self.assertIsNotNone(reference(text), name)
            self.assertFalse(self.verdicts[name]['accepted'], name)

    def test_invalid_toml_is_rejected_by_both(self):
        for name, text in BOTH_REJECT.items():
            self.assertIsNone(reference(text), name)
            self.assertFalse(self.verdicts[name]['accepted'], name)


if __name__ == '__main__':
    unittest.main()
