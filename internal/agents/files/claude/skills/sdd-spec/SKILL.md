---
description: Draft or continue the functional spec of a feature with sdd (Spec-Driven Development). Use when the user wants to specify a new feature, or to keep working on a functional spec.
argument-hint: <feature name, or a description of the feature>
---

Draft the functional spec of this feature with the user: $ARGUMENTS

Use the `sdd` MCP tools (if they are not available, run the equivalent `sdd` commands in the shell).

1. If no feature was given, call `status` and ask the user which feature to work on, or what new feature to specify.
2. Call `guide` with topic `functional` (and the feature, if it already exists) and follow it.
3. If the feature does not exist yet, create it with `spec_new`, passing the user's words above as `text`.
4. Interview the user and write the spec into the file as you go. Call `lint` after every edit and fix the errors.
5. When lint passes and the user agrees the spec is complete, stop and ask the user to approve it in their terminal with `sdd spec approve <feature> --functional`. Never run an approval command yourself.
