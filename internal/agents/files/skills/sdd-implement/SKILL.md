---
name: sdd-implement
description: Implement the next tasks of a feature with sdd (Spec-Driven Development) in its isolated worktree, until the current group is verified and waits for the user's review. Use when the user wants to implement or continue implementing an approved plan.
argument-hint: [feature]
---

Implement a feature with sdd.

Feature: {{args}}

Use the `sdd` MCP tools (if they are not available, run the equivalent `sdd task` / `sdd group` commands in the shell).

1. If no feature was given, call `status`: use the feature being implemented if there is only one, otherwise ask the user which one.
2. Call `guide` with topic `implement` once, and follow it.
3. Call `task_next` with the feature and do what it says: implement the task in the worktree it names, then call `task_done`; repeat. When it asks for it, call `group_verify` and fix any failure.
4. Stop as soon as `task_next` or `group_verify` says to stop: the group is waiting for the user's review (`sdd review G<n>`), it failed, or nothing is left. Tell the user what happened and what to run next: {{tell}}.

If `status` shows the feature is not being implemented yet, ask the user to run `sdd implement <feature>` first. Never run `sdd approve`, `sdd revise`, `sdd resume`, `sdd clean` or `sdd merge` yourself.
