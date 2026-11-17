---
description: Adopt an existing codebase into sdd (Spec-Driven Development) by documenting its current behavior and design as baseline specs, without changing the code. Use when the user wants to start using sdd on a project that already has code.
argument-hint: [area or module to adopt]
---

Adopt this existing project into sdd: $ARGUMENTS

Use the `sdd` MCP tools (if they are not available, run `sdd adopt --context` and the other `sdd` commands in the shell).

1. Call `guide` with topic `adopt` and follow it.
2. Call `adopt_context`, explore the code, and propose to the user how to split it into baseline features. Wait for their agreement.
3. For each baseline feature, document the current behavior (functional spec, `spec_new` with `baseline: true`) and then the current design (technical spec), calling `lint` (and `sync_check` for the technical spec) after every edit. After each spec, stop and ask the user to approve it in their terminal.
4. Never change code during adoption, and never run an approval command yourself.
