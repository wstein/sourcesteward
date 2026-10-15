# Validate site themes

1. Run `make check` with Go, Python 3 and Node available; behavior/security/packaging tests must pass.
2. Serve the repository with `python3 -m http.server 8765` and open `/docs/`.
3. Clear the SourceSteward theme preference. With system light/dark, System should follow that preference.
4. Keyboard-select Light and Dark, reload each, and confirm the selection/palette persists. Return to System.
5. Disable storage, then repeat selection and confirm current-visit operation. Set an invalid saved preference and reload; expect System.
6. Disable JavaScript and reload with both system preferences; content remains readable and the inactive switch is hidden.
7. Check both palettes at 375×812 and 1280×800: no horizontal overflow, visible keyboard focus, readable controls/content. Measure text contrast.
8. Record actual evidence in verification.md. Independent review and PR handoff follow acceptance; this guide does not grant publication authority.
