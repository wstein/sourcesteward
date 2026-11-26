# Changelog

## 0.6.0

### New since 0.5.0

- **Watch branch pushes.** `stw watch --all` includes each repository's default
  branch. Repeat `--branch NAME` to select named branches, or use `--all-branches`
  to watch every branch.
- **Customize watch output per event.** Define templates in `watch.toml`, load a
  separate TOML file with `--template`, or override individual events with
  `--template-string KIND=TEXT`. JSON Lines remains the default and the
  JSON journal retains all events when templates are enabled.
- **Install without Homebrew.** The release installer checks the archive's
  SHA-256 before installing `stw` into a user-writable directory on macOS
  and Linux.
- **Find guides and agent instructions.** The documentation site has dedicated
  guides, and CLI help links to the agent guide.
