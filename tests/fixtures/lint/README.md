# Fixtures of `stw lint review` and `stw lint pr-text`

Real texts, stored unchanged, read from GitHub on 2026-11-17 (`gh api repos/wstein/<repo>/issues/<n>/comments`, GET only).
No test asks GitHub.

| File | Source |
| --- | --- |
| `review-cubelab-49-clear.txt` | comment 6955623075 of wstein/cubelab#49, `Review: CLEAR`, head `82dc939a0115b4f116c8ac71fb73eb3b96a1d01b`, patch-id `8a7ecc88da6cff3f867a0e932184ca39f5bd151a` |
| `review-dotagents-99-findings.txt` | comment 6955542956 of wstein/dotagents#99, `Review: FINDINGS` for the older head `dd25b136896a8af84d0cf1adf43e69db1c2e70fe` |
| `review-dotagents-99-clear.txt` | comment 6955742413 of wstein/dotagents#99, `Review: CLEAR`, head `13cacbe99b273d46c234efdd7662f3a4410472e0`, patch-id `4dd58dd32fc07562d0a26f316bca59669657a52c` |
| `carried-over-cube-assembler-61.txt` | comment 6955778060 of wstein/cube-assembler#61, `Carried-over` from `5da6a04090ad10bb55a9a012cd17c4fa2755a1a6` to `bfbf79cc1d3372c874bd7c3db028a416e3208f94`, patch-id `c9d382d0ef112f55ea237acfc0d8bb3e380c46b5` |
| `pr-text-cubelab-49.txt`, `pr-text-dotagents-99.txt`, `pr-text-cube-assembler-61.txt` | the PR text (`issues/<n>` body) of the three merged pull requests; `gh` adds one final line feed |
