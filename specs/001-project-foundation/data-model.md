# Data Model: Project Foundation

## VersionInformation — implemented
- schema_version: integer, exactly 1 for the initial JSON contract.
- name: string, exactly SourceSteward.
- version: nonempty string; development builds report dev.
Text and JSON derive from the same value. Version reporting has no repository state transition.

## BackendIdentity and capabilities — design boundary only
A future selected backend is git, jj or gitbutler with executable/version, managed workspace
identity, capabilities and disclosed inspection/mutation effects. Git commit OIDs, jj change
IDs and operation IDs, and GitButler identifiers remain typed and namespaced; they are not
interchangeable strings or assumed to have the same stability.

A common result may include filesystem changes, diagnostics and operation outcomes. Backend
extensions preserve bookmarks, branches, stacks and backend-specific evidence independently.
No cleanup plan persistence or backend adapter is implemented in foundation. See docs/backends.md
for selection, stale-plan validation and unsupported-operation requirements.

## PublicationRevision — workflow
The same github.sha identifies the checked source, deployment checkout and revision.txt
inside the website artifact. State transition: source checks fail → no deployment; source checks
pass on a main push → package that SHA → upload → deploy using the Pages environment.
