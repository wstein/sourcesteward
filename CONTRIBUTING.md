# Contributing to SourceSteward

Use [GitHub Issues](https://github.com/wstein/sourcesteward/issues) for bugs,
questions, and feature requests. Report vulnerabilities through the
[security policy](SECURITY.md).

## Prepare a change

- Create a topic branch from `main`. Fork the repository if needed.
- Make focused [Conventional Commits](https://www.conventionalcommits.org/),
  for example `fix(watch): preserve the branch snapshot on read failure`.
- Run the relevant tests. For changes that write refs, the index, or the
  worktree, use a disposable repository and cover successful and refused writes.
- When CLI help changes, regenerate the command reference.

```sh
cargo fmt --check
cargo test --locked
cargo run --locked --quiet --bin stw -- dev command-reference > docs/command-reference.md
```

## Open a pull request

Push your topic branch and open a pull request against `main`. Describe the
change, list the checks you ran, and explain any effects on Git state. Give
the reason for any skipped checks. Address review feedback on the topic branch.
The repository owner merges pull requests and publishes releases.
