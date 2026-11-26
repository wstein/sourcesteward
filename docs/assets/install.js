// SPDX-License-Identifier: EUPL-1.2
// Progressive enhancement: without this script both install panels stay visible.
(() => {
  'use strict';
  const root = document.querySelector('[data-install]');
  if (!root) return;
  const tabs = [...root.querySelectorAll('[role="tab"]')];
  const panels = [...root.querySelectorAll('[role="tabpanel"]')];
  if (tabs.length !== 2 || panels.length !== 2) return;

  function select(index) {
    tabs.forEach((tab, i) => {
      const on = i === index;
      tab.setAttribute('aria-selected', on ? 'true' : 'false');
      tab.tabIndex = on ? 0 : -1;
    });
    panels.forEach((panel, i) => {
      panel.hidden = i !== index;
    });
  }

  root.classList.add('is-enhanced');
  select(0);
  tabs.forEach((tab, index) => {
    tab.addEventListener('click', () => {
      select(index);
      tab.focus();
    });
    tab.addEventListener('keydown', (event) => {
      if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
      event.preventDefault();
      const next = event.key === 'ArrowRight' ? (index + 1) % tabs.length : (index + tabs.length - 1) % tabs.length;
      select(next);
      tabs[next].focus();
    });
  });
})();
