---
description: Draft or continue the implementation plan of a feature with sdd (Spec-Driven Development), once its functional and technical specs are approved. Use when the user wants to plan how an approved feature will be built, task by task.
argument-hint: <feature>
---

Draft the implementation plan of this feature with the user: $ARGUMENTS

Use the `sdd` MCP tools (if they are not available, run the equivalent `sdd` commands in the shell).

1. If no feature was given, call `status` and ask the user which feature to plan.
2. Call `status` for the feature: both specs must be approved. If they are not, tell the user what is missing and stop.
3. Call `guide` with topic `plan` and the feature, and follow it. Its coverage checklist lists every ID the plan must cover.
4. If `plan.md` does not exist yet, create it with `plan_new`.
5. Propose the groups to the user, then write the plan. Call `lint` after every edit and fix the errors.
6. When lint passes and the user agrees, stop and ask the user to approve it in their terminal with `sdd plan approve <feature>`. Never run an approval command yourself.
