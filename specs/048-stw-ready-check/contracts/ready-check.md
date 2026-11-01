# Contract: `stw ready-check --json` (Spec 048)

Unpublished `schemas/v1/ready-check.schema.json`, byte-frozen by digest in `internal/contracts/schema_freeze_test.go` and excluded from
Pages packaging until the feature is accepted. `schema_version` is 1; consumers must ignore unknown members. Keys are never omitted;
absent values are `null`.

Members: `schema_version` 1, `command` `ready-check`, `time` (RFC 3339 UTC), `host`, `repo`, `number`, `head` (40 hex, or null when
`pulls/N` could not be read), `draft` (bool or null), `ready` (bool), `conditions` (exactly seven objects `{id, status, evidence}` in the
order `open`, `review`, `checks`, `base`, `blocker`, `conflict`, `rebaseable`; `status` `pass|fail|unknown`; `evidence` at most 400
characters, control characters escaped) and `errors` (`{source, code, message}` per failed read; `source` `pull|repo|checks|review`).
`ready` true implies all conditions `pass`, a head and no error.

```json
{"schema_version":1,"command":"ready-check","time":"2026-11-01T09:00:00Z","host":"github.com","repo":"wstein/sourcesteward","number":343,"head":"3f2a9c1000000000000000000000000000000000","draft":true,"ready":true,"conditions":[{"id":"open","status":"pass","evidence":"state open"},{"id":"review","status":"pass","evidence":"CLEAR for head 3f2a9c1 per comment"},{"id":"checks","status":"pass","evidence":"all check runs and statuses of head 3f2a9c1 are green"},{"id":"base","status":"pass","evidence":"base main is the default branch"},{"id":"blocker","status":"pass","evidence":"no Blocker line names a blocker"},{"id":"conflict","status":"pass","evidence":"no conflict with the base (state draft)"},{"id":"rebaseable","status":"pass","evidence":"rebaseable is true"}],"errors":[]}
```

Human output: one line `OWNER/NAME#N head SHA7, draft BOOL`, one line per condition (`id`, status, evidence), the read errors, and a
final `Ready: yes` or `Ready: no`.
