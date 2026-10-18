# Planned Ready inspection workflow

This is a design walkthrough. The command, schemas and publisher are not implemented or installed.

1. Complete independent specification, plan and cross-artifact review gates. Implement test-first only after complete design CLEAR.
2. Author an explicit project policy and owner-controlled lock. Configure real trusted issuer IDs and exact tier/model mappings; never use the fictitious shape examples as authority.
3. An independently executed full-PR review supplies a public structured report. The accountable issuer publishes its exact-byte digest in the configured head status. Declare configured model metadata honestly; do not claim runtime attestation.
4. Once implemented and separately authorized for the installed artifact, inspect one PR:

   ```sh
   stw hosting ready-check 42 /path/to/repository --policy-lock /trusted/project-ready-lock.json --json
   ```

   The selected lock is an explicit trust input. Missing required proof produces a nonzero result. The CLI changes no Ready flag and publishes nothing.
5. Compare the [deployment profiles](contracts/github-check.md). Independently establish current platform capabilities and credential/source authenticity. Present the smallest concrete owner operation; no App, secret, environment or settings change is preauthorized by the feature request.
6. After the chosen trusted publisher exists and passes its security acceptance, prepare the exact required-check configuration diff and obtain the owner's activation operation. The check protects merge; people may still manually change the Ready UI flag.

No polling, schedule, release build, documentation build or consumer activation follows from these steps. Subsequent head/base/policy changes require fresh valid evidence; the last sample is not an atomic guarantee about a later merge.
