# Help Discovery Contract

| Invocation | Notice | Successful status |
| --- | --- | --- |
| `stw` | Once | 0 |
| `stw --help` | Once | 0 |
| `stw -h` | Once | 0 |
| `stw help` | Once | 0 |
| `stw version` | Absent | 0 |
| `stw version --json` | Absent; existing JSON object unchanged | 0 |
| `stw version --help` | Absent | 0 |
| `stw help version` | Absent | 0 |
| Unknown command/flag/help topic | Absent | 2 |

The notice uses the SheetMill discovery pattern adapted to this product. Required literal wording:

```text
Are you an AI? Use these resources ONLY IF your task specifically asks you to:
  Help a human understand, set up or use SourceSteward:
    https://raw.githubusercontent.com/wstein/sourcesteward/main/docs/agent-guide.md
  Debug or investigate a problem with SourceSteward:
    https://raw.githubusercontent.com/wstein/sourcesteward/main/llms.txt
```

Both exact destinations match [data-model.md](../data-model.md). No notice retrieval occurs. Existing standard help output remains intact; append once only after ordinary root-help rendering succeeds.

Any ordinary-help or notice write failure returns 1 through the existing diagnostic boundary. Cancellation retains status 1. The guide documents these distinctions and does not promise unavailable inspection, cleanup, recovery or backend commands.
