# Foundation verification

## Source and local acceptance — 2026-10-15

Implementation source revision: fd4d652ce402621bf07b4d83ee5a5da7b70f4fd3.
The later evidence update changes documentation only.

- CLI tests first failed with missing implementation, then passed after implementation.
- Independent review reproduced invalid help topics succeeding and protocol-relative runtime
  assets bypassing offline checks. Both regression tests failed before their fixes and pass now.
- The same independent reviewer rechecked the final source revision and reported no remaining
  actionable source findings. This is source review, not a full security/accessibility audit.
- make check passed: formatting, Go tests/vet/module verification, five Python contract tests,
  authored links/assets/SVG/font notices, feature evidence pointers, architecture and whitespace.
- go test -race ./... and actionlint passed. CI also verified go.mod/go.sum remain tidy.
- Six actual stw process invocations passed outside a repository with Git absent from PATH:
  help, text version, JSON version, invalid help topic, extra help argument and unsupported flag.
  Invalid inputs returned exit 2, empty stdout and exactly one diagnostic.
- The first commit, 79963dd, has an empty tree. Its historical initialization name is retained.

## Browser and branding acceptance

Author browser inspection covered the final SourceSteward name, stw examples, original open
branch/guide-rail mark, desktop 1280×800 and mobile 375×812. document scrollWidth matched
viewport width at both sizes. Fonts loaded locally; keyboard Tab focused the skip link with
visible outline. Static links and anchors passed offline validation. Reduced-motion CSS is
present; no full assistive-technology audit or Windows execution coverage is claimed.

## GitHub and live publication

[Initial CI/Pages run](https://github.com/wstein/sourcesteward/actions/runs/38743805161)
succeeded for the exact implementation revision. The live
[revision file](https://wstein.github.io/sourcesteward/revision.txt) reported that SHA, and
HTML/CSS/logo bytes matched the local checked sources after publication.

Verified static SHA256 values:
- index.html: 2dd99fb7635bb6d2c9adf2271983e1d0b45e2e4f31fc397b42c84984d44433d0
- assets/logo.svg: 0bd9fbaae193e88cb0e5b713a8f0b0ace60f6c3fdbdb2cb0c100209942f4eb6d
- assets/site.css: b516e066ac2cc22efcf9233a0c2df3da2497733176bc7ea29e33af3887ff7256

Repository metadata includes description, topics and Pages homepage. Pages uses Actions with
HTTPS. GitHub's community profile reported 100%; private vulnerability reporting is enabled.
Issue forms, PR guidance, owner review, conduct/security policies and weekly Dependabot are
present. A durable local checkout was cloned from the published repository.

## Scope and remaining work

Help/version only are implemented. Git, jj and GitButler adapters, cleanup, recovery and MCP
remain planned. No CLI release or user-repository mutation was performed. Future adapters
need their own specifications, acceptance fixtures and platform coverage. The evidence commit
is published through the same CI/Pages gate; revision.txt always identifies the deployed SHA.
