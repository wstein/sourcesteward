# Development harness

This repository applies the user's selected engineering references to a small CLI project.
It does not install a continuous agent runner, grant merge authority or force a particular model.

## Session protocol
1. Read AGENTS.md, docs/progress.md and the active feature's spec/plan/tasks.
2. Inspect git status and recent commits; preserve other people's work. Run make check to
   establish the current baseline. Record existing failures before changing code.
3. Pick one scoped task. State observable acceptance criteria before implementing it.
4. Write failing behavior tests, implement, and exercise the real entry point. Site changes
   also require browser inspection at desktop and narrow widths.
5. For consequential changes, obtain an independent reviewer/evaluator where available;
   provide the exact revision, acceptance criteria and evidence, and fix concrete findings.
6. Run checks, make focused Conventional Commits within task authorization, then update
   tasks, docs/progress.md and feature-status.json. Preserve verification failures and gaps.

A handoff records current revision, completed work, decisions, tests actually run, remaining
failures and the next bounded action. A new session resumes from repository artifacts rather
than assuming chat history is complete. Planned features remain planned until acceptance passes.

## Mechanical feedback
make check verifies command behavior, docs links/assets, feature-status evidence pointers,
module integrity and whitespace. Architecture checks prohibit repository/network execution
inside the foundation CLI. CI adds module-tidiness and race checks. Pages deploys only after
same-run checks on the exact main-push SHA. Dependency updates require the same checks.

## Design evaluation
Evaluate coherence with family branding, original mark/layout decisions, typography/contrast
and actual usability. Check keyboard navigation, mobile overflow, links and current/planned
claims. Record findings and limitations; a self-awarded score is not independent review.
Scale evaluator effort to risk and task difficulty. Keep the harness small and revisit gates
when measured failures or tool/model capabilities change.

## References and application
[OpenAI: Harness engineering](https://openai.com/index/harness-engineering/) informs the short
agent map, repository-local knowledge, architecture boundaries and executable feedback checks.
[Anthropic: Harness design](https://www.anthropic.com/engineering/harness-design-long-running-apps)
informs explicit acceptance criteria, durable handoffs and independent evaluation of actual
behavior. These are engineering inputs, not permission to adopt their deployment/merge policies.
