---
name: sdd-tech
description: Draft or continue the technical spec of a feature with sdd (Spec-Driven Development), once its functional spec is approved. Use when the user wants to design how an approved feature will be built.
argument-hint: <feature>
---

Draft the technical spec of a feature with the user.

Feature: {{args}}

Use the `sdd` MCP tools (if they are not available, run the equivalent `sdd` commands in the shell).

1. If no feature was given, call `status` and ask the user which feature to work on.
2. Call `status` for the feature: its functional spec must be approved. If it is not, tell the user what is missing and stop.
3. Call `guide` with topic `technical` and the feature, and follow it.
4. If `technical.md` does not exist yet, create it with `spec_new` (`technical: true`).
5. Study the code base, agree on the design with the user, and write the spec. Call `lint` and `sync_check` after every edit and fix the errors.
6. When both pass and the user agrees, stop and ask the user to approve it in their terminal with `sdd spec approve <feature> --technical`. Never run an approval command yourself.
