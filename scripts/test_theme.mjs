// SPDX-License-Identifier: EUPL-1.2
// Exercise the actual browser asset without adding a DOM/test dependency.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const source = readFileSync(new URL('../docs/assets/theme.js', import.meta.url), 'utf8');
const key = 'sourcesteward.theme';

function visit({ saved = null, blockedRead = false, blockedWrite = false } = {}) {
  const attributes = new Map();
  const listeners = new Map();
  const storage = new Map(saved === null ? [] : [[key, saved]]);
  const control = { hidden: true };
  const select = {
    value: 'system',
    addEventListener(event, callback) { listeners.set(event, callback); },
  };
  vm.runInNewContext(source, {
    document: {
      documentElement: {
        setAttribute(name, value) { attributes.set(name, value); },
        removeAttribute(name) { attributes.delete(name); },
      },
      getElementById(id) { return id === 'theme-control' ? control : id === 'theme-select' ? select : null; },
    },
    window: {
      localStorage: {
        getItem(name) { if (blockedRead) throw Error('storage blocked'); return storage.get(name) ?? null; },
        setItem(name, value) { if (blockedWrite) throw Error('storage blocked'); storage.set(name, value); },
        removeItem(name) { if (blockedWrite) throw Error('storage blocked'); storage.delete(name); },
      },
    },
  });
  return {
    attributes, storage, control, select,
    choose(value) { select.value = value; listeners.get('change')(); },
  };
}

test('first visit follows system without overriding CSS', () => {
  const page = visit();
  assert.equal(page.select.value, 'system');
  assert.equal(page.attributes.has('data-theme'), false);
  assert.equal(page.control.hidden, false);
});

for (const theme of ['light', 'dark']) {
  test(`saved ${theme} is selected and applied`, () => {
    const page = visit({ saved: theme });
    assert.equal(page.select.value, theme);
    assert.equal(page.attributes.get('data-theme'), theme);
  });
  test(`choosing ${theme} persists and survives a new visit`, () => {
    const page = visit();
    page.choose(theme);
    assert.equal(page.attributes.get('data-theme'), theme);
    assert.equal(page.storage.get(key), theme);
    assert.equal(visit({ saved: page.storage.get(key) }).select.value, theme);
  });
}

test('system removes saved preference and explicit override', () => {
  const page = visit({ saved: 'dark' });
  page.choose('system');
  assert.equal(page.select.value, 'system');
  assert.equal(page.attributes.has('data-theme'), false);
  assert.equal(page.storage.has(key), false);
});

for (const saved of ['invalid', 'system', '', '<script>']) {
  test(`unrecognized saved value ${JSON.stringify(saved)} follows system`, () => {
    const page = visit({ saved });
    assert.equal(page.select.value, 'system');
    assert.equal(page.attributes.has('data-theme'), false);
  });
}

test('blocked storage read and write leave current visit functional', () => {
  const page = visit({ blockedRead: true, blockedWrite: true });
  assert.equal(page.select.value, 'system');
  assert.equal(page.control.hidden, false);
  page.choose('dark');
  assert.equal(page.attributes.get('data-theme'), 'dark');
  page.choose('light');
  assert.equal(page.attributes.get('data-theme'), 'light');
  page.choose('system');
  assert.equal(page.attributes.has('data-theme'), false);
});

test('blocked storage getter still permits theme selection', () => {
  const attributes = new Map();
  let change;
  const select = { value: 'system', addEventListener(event, callback) { change = callback; } };
  const control = { hidden: true };
  vm.runInNewContext(source, {
    document: {
      documentElement: { setAttribute(k,v) { attributes.set(k,v); }, removeAttribute(k) { attributes.delete(k); } },
      getElementById(id) { return id === 'theme-control' ? control : select; },
    },
    window: Object.defineProperty({}, 'localStorage', { get() { throw Error('storage denied'); } }),
  });
  assert.equal(control.hidden, false);
  select.value = 'dark';
  change();
  assert.equal(attributes.get('data-theme'), 'dark');
});
