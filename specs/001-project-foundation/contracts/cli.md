# CLI Contract: Foundation

Executable and standalone command: `stw`. Repository: wstein/sourcesteward.
Commands below are implemented by this milestone; all roadmap operations remain absent.

| Invocation | stdout | stderr | Exit |
| --- | --- | --- | --- |
| no arguments / --help / help | human help | empty | 0 |
| version | SourceSteward dev followed by newline | empty | 0 |
| version --json | version object followed by newline | empty | 0 |
| unknown command/flag/extra args | empty | one Error: diagnostic line | 2 |
| output failure or cancelled context | no success promise | one Error: diagnostic line, if writable | 1 |

JSON example:
```json
{"schema_version":1,"name":"SourceSteward","version":"dev"}
```

A command constructor/execution boundary receives its build version, args, context and
stdout/stderr writers. Repeated executions do not share flags. No command reads stdin,
starts editors/pagers, runs Git/jj/GitButler, accesses a network or changes repository state.
Help is human-readable; --json is supported only for version in this milestone.
Runtime/output errors must not be misclassified as usage errors. An unwritable stderr can
prevent the diagnostic being delivered but does not turn failure into success.
