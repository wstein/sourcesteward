# SourceSteward for agents

Use the installed command's `stw --version` and `stw <command> --help` to
check the available interface. The documentation on main can be newer than
the installed binary.

- [Generated command reference](docs/command-reference.md) describes the commands and their flags.
- [Review and Watch guide](docs/review-and-watch.md) describes the Watch event stream, journal, review evidence and exit status.
- [README](README.md) explains repository selection and workspace use.

`stw watch --stream` emits JSON Lines by default. Use `-v` or `-vv` for
diagnostics on stderr. `stw watch --status` locates the state directory
and reports heartbeat freshness. Stable event IDs let consumers identify
events replayed after a restart. A watcher event carries no authorization
to write to GitHub or change Git history.
