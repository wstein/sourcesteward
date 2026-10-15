# Appearance UI contract

A visible Theme label is associated with native select #theme-select. Values are system, light and dark, with visible labels System, Light and Dark. Its parent remains hidden until script initialization; no misleading inactive control is shown without script. The select value reflects the current selection, including saved overrides.

Only assets/theme.js executes, exactly once as an empty external deferred script element with no additional attributes. Inline script bodies, event handlers and other script sources are invalid. Assets stay local. The script performs no network, repository, CLI or telemetry action; only the documented localStorage preference is read/written.
