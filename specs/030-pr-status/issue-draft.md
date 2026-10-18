# Feature-only body reference — issue #74

Title: **Extend PR inspection with lifecycle evidence, compact output and bounded watch**

Deduplication on 2026-10-18: repository issue search for `hosting pr status` returned only the distinct Ready inspection item; searches for `expect-head` and `hosting watch` returned no matches. The coordinator subsequently created [issue #74](https://github.com/wstein/sourcesteward/issues/74); this file retains the sanitized feature body. This author made no issue write.

---

`stw hosting pr` already reads a numbered GitHub PR. Extend that read-only operation so callers can distinguish an unmerged closure from a merge, bind an observation to an expected head and explicitly wait within finite limits.

Acceptance criteria:

- Report merged/closed evidence and dates, keeping the head distinct from GitHub's method-dependent result SHA. A test-merge SHA on an open PR must never imply integration.
- Add compact single-line human output while retaining detailed output, and an explicit versioned JSON contract with nullable fields, timestamps and documented v1 migration.
- Add full expected-head validation; mismatch is an operational failure and conveys no review or merge permission.
- Add watch only by explicit flag, with a global deadline, finite requests/backoff, cancellation and one final output. Resolve output-delivery bounds and ordinary terminal/file usability before claiming completion.
- Preserve native Git/jj ownership, fixed read-only GETs, credential redaction and existing exit-status conventions. Add behavior-first fixtures and conformance evidence.

No provisioning, generic gh wrapper, Ready evaluation or hosting mutation is included. Specification and independent design review precede implementation.

🤖 Generated with [Codex](https://openai.com/codex/)
