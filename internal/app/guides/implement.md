# Implementing a feature

The user starts the implementation with `sdd implement <feature>`, which creates an isolated git worktree (`.sdd/worktrees/<feature>`, branch `sdd/<feature>`). You implement the plan there one task at a time; sdd verifies, commits and stops for the user's review after each group.

## The loop

1. Call `task_next` with the feature. It returns one of:
   - a **task**: what to build, the files to touch, the spec text it covers, the worktree path, the commands and the language profile;
   - **revision feedback** from the user: address it in the worktree, then call `group_verify`;
   - a request to call `group_verify` (every task of the group is done);
   - a reason to **stop**: the group awaits the user's review, the group failed, or nothing is left. Stop and tell the user what `task_next` said.
2. Implement the task in the worktree, with unit tests that reference the criteria they verify (`covers: AC-###` in a comment or the test name; in a file this feature does not otherwise change, `covers: <feature>/AC-###`). Run the tests yourself as you go.
3. Call `task_done` with a one-line summary. sdd runs the build; if it fails, fix it and call `task_done` again. When it passes, sdd commits a checkpoint.
4. Repeat from 1. When `task_next` asks for it, call `group_verify`: sdd runs build, lint and test. Fix failures and call it again; after the configured number of failures the group is marked failed and you must stop.
5. When `group_verify` passes, sdd squashes the group into one commit and the group waits for the user's review. **Stop** and ask the user to run `sdd review G<n>` (optionally the `sdd-review` skill first, for an expert review); they approve (`sdd approve G<n>`) or send feedback (`sdd revise G<n>`). Do not start the next group yourself before they do.

## Rules

- Work only inside the worktree path that `task_next` gives you. Never edit the main checkout, and never edit the specs or the plan while implementing (that makes them stale and stops the implementation).
- Do not commit, amend, rebase or switch branches in the worktree: sdd owns the history.
- Keep to the task's declared files where you can; every other changed file is reported to the user in the review.
- Follow the language profile and the project's existing conventions.
- Never run `sdd approve`, `sdd revise`, `sdd resume`, `sdd clean` or `sdd merge`: they belong to the user.
- If the session is interrupted, call `task_next` again: it returns the task in progress together with any uncommitted changes.
