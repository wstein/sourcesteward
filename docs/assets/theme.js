// SPDX-License-Identifier: EUPL-1.2
// Progressive enhancement only: CSS supplies system appearance without JavaScript.
(() => {
  'use strict';
  const key = 'sourcesteward.theme';
  const root = document.documentElement;
  const control = document.getElementById('theme-control');
  const select = document.getElementById('theme-select');
  if (!control || !select) return;

  function apply(value) {
    if (value === 'light' || value === 'dark') {
      root.setAttribute('data-theme', value);
    } else {
      root.removeAttribute('data-theme');
    }
  }

  let saved;
  try { saved = window.localStorage.getItem(key); } catch { /* Storage may be unavailable. */ }
  select.value = saved === 'light' || saved === 'dark' ? saved : 'system';
  apply(select.value);
  select.addEventListener('change', () => {
    apply(select.value);
    try {
      if (select.value === 'system') window.localStorage.removeItem(key);
      else window.localStorage.setItem(key, select.value);
    } catch { /* Keep the current visit functional when storage is blocked. */ }
  });
  control.hidden = false;
})();
