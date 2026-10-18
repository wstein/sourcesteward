# Research: PR status

Inspected on 2026-10-18; official documentation, no live repository requests.

[GitHub REST: Get a pull request](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request) documents a nullable `mergeable` while background computation runs. It describes `merge_commit_sha` as a test-merge identity before merging. After merge its meaning depends on the method: merge commit, squash result, or the commit to which rebasing updated the base. The field is reported evidence, not proof of current base ancestry or review validity.

[GitHub: Merging a pull request](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/merging-a-pull-request) documents that GitHub rebase-and-merge creates new commit SHAs. Therefore reviewed head and reported post-merge result must remain separate identities.

Documentation is semantic evidence only. Live Enterprise shapes, permissions and timing have not been measured here. Existing transport pins REST API version 2022-11-28; schema field presence/nullability must also be checked against that supported interface before the final gate.
