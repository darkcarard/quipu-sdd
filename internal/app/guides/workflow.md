# Working with sdd

This project uses `sdd` for Spec-Driven Development. Every change starts as a feature in `specs/<feature>/`: a functional spec (what and why), a technical spec (how), then a plan of small tasks, then the implementation. You draft and implement; `sdd` checks everything deterministically; the user approves each step.

How to work:
- Use the `sdd` MCP tools (`status`, `guide`, `spec_new`, `plan_new`, `lint`, `sync_check`, `trace`). If they are not available, run the equivalent `sdd` commands in the shell (`sdd status`, `sdd guide <topic>`, `sdd spec new`, `sdd spec lint`, `sdd sync check`, `sdd trace`).
- Before drafting an artifact, call `guide` for its topic and follow it.
- After every edit to a spec or the plan, run `lint` and fix the errors before handing back to the user.
- Approvals belong to the user. Never run `sdd spec approve`, `sdd plan approve`, `sdd approve` or `sdd revise`; when an artifact is ready, ask the user to run the command that `status` reports as next.
- Editing an approved spec reopens it and makes everything derived from it stale. Do it only when the user asks, and say so.
- Do not edit `sdd.yaml` or anything under `.sdd/` (state, journal, templates) unless the user asks.
- Feature IDs (`FR-001`, `AC-001`, …) are permanent: never renumber or delete them; retire them instead (see the guides).

Phases: functional spec → technical spec → plan → implementation → validation. `status` always tells you the phase of a feature and the next step.
