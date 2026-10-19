# Quickstart (design target; no command below exists yet)

```text
stw tidy plan . --contract 3 --base main --json
stw tidy plan . --contract 3 --base main --integration-proof ancestor,merge-noop --json
stw tidy plan . --contract 3 --base main --report-dir /abs/review --consolidation-range main~40..main
stw tidy plan . --contract 3 --base main --decisions /abs/review/decisions.json --json
stw tidy plan . --contract 3 --base main --online --remote origin --json
stw tidy plan . --contract 3 --base main --online --remote origin --pr FULL_OID=123 --json
```

Expected: the first plan lists per-ref class, reasons and counts with no network. The third writes `tidy-report.md` (for the human) and `tidy-report.json` (for an AI reviewer) into the owner-named directory. The reviewer writes `decisions.json`; the fourth plan makes `integrated` and `drop` deletable as `reviewed-integrated` and `reviewed-drop`. `apply` keeps Spec 029 authorization and recovery; a decision is never authorization. A repository with `submodule.*` or `core.hooksPath` plans with a configuration note. Running any of this with a built artifact needs separate owner authorization.
