# Quality and known gaps

| Domain | Evidence | Gap |
| --- | --- | --- |
| CLI contract | table-driven Go tests, writer failure and cancellation checks | Identity inspection only; no status or mutations |
| Structured output | published schema 1 contracts, real CLI output validation and negative invariants | Live schema URLs require approved merge and successful Pages publication |
| Documentation | local links, assets, SVG and evidence pointers checked offline | External destinations can change; no full accessibility audit |
| Architecture | forbidden effects/dependency imports checked mechanically | Future adapter/application layers require new constraints |
| Website | desktop/mobile browser acceptance and exact-SHA CI gate | Initial landing page; no documentation search |
| Backends | jj-ready design contract | Git identity only; native jj/GitButler adapters unimplemented |
| Dependencies | pinned modules, verification and Dependabot | Automated update proposals still require review |

Do not interpret absence of a known failure as proof of feature completion. Record commands,
revision and remaining platform/behavior gaps in verification.md.
