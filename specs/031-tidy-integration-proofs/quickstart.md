# Quickstart (design target; no command below exists yet)

```text
stw tidy plan . --contract 3 --base main --json
stw tidy plan . --contract 3 --base main --integration-proof ancestor,merge-noop,patch-exact --json
stw tidy plan . --contract 3 --base main --online --remote origin --json
stw tidy plan . --contract 3 --base main --online --remote origin --pr FULL_OID=123 --json
```

Expected: the first plan lists per-ref proof kind, strength and counts with no network; the third discovers merged PRs per distinct OID; the fourth overrides one OID. A repository with `submodule.*` or `core.hooksPath` plans with a configuration note. `apply` keeps Spec 029 authorization and recovery. Running any of this against a built artifact needs separate owner authorization.
