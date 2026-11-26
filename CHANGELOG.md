# Changelog

All notable changes to Quipu (`sdd`). Versions follow [semantic versioning](https://semver.org); before 1.0, a minor version can add features and change behavior.

To update: `go install github.com/darkcarard/quipu-sdd/cmd/sdd@latest`, then `sdd init --agent <your agents>` in each project to add the new integration files (see the README).

## Unreleased

### Added
- An unqualified `covers:` reference in a test file that another feature's plan also declares, to an ID both features define, is reported as ambiguous by `sdd sync check` and `sdd validate` (a warning that suggests `covers: <feature>/AC-###`).
- **Errata after the merge.** Editing a merged feature's spec or plan keeps it `validated`; approving the edit (same IDs) restamps the derived artifacts instead of asking to re-approve the plan. An edit that adds, deletes or renumbers IDs is refused: new behavior goes in a new feature.
- `sdd spec approve` and `sdd plan approve` refuse when the artifact was edited in the feature's worktree and differs from the project checkout's copy, which is the one they approve; `--from-worktree` copies the worktree's version first.
- Spec lint reports an ID written right after another feature's name (`mock-domain FR-012`) and suggests `mock-domain/FR-012`; sync checks that a `<feature>/<ID>` reference names an existing feature that defines the ID.

### Fixed
- Re-approving the specs and plan of a merged feature moved it back to `plan-approved`, and `sdd status` suggested `sdd implement`. A merged feature now always shows as `validated` (states already affected are repaired when loaded).
- `sdd validate` on a merged feature recreated the worktree from the old branch and left its records uncommitted. It now runs in the project checkout, removes a clean leftover worktree and commits the records; `sdd status` points to `sdd clean` when a worktree is left.
- Unqualified `covers:` references in a test file shared by several features were reported as "not defined" or "ambiguous" in every feature that touched the file. In a file another feature's plan declares, a reference now belongs to the feature whose commit wrote the line (its `SDD-Feature` trailer, via `git blame`); only lines no feature wrote are still reported as ambiguous.

## v0.2.1 (2026-11-25)

### Fixed
- Re-approving the specs and the plan of a feature whose groups were already approved left it in `plan-approved`, so `sdd validate` refused to run until an extra `sdd implement`. The feature now returns to `implemented` (or `implementing` when the re-approved plan has groups still to do).
- `covers:` references are scoped to their feature: an unqualified `covers: AC-001` counts only in files the feature changed, so another feature's tests no longer satisfy (or trigger warnings about) this feature's criteria. Qualify a reference as `covers: <feature>/AC-001` to use it from any file.

## v0.2.0 (2026-11-23)

### Added
- **Side-by-side diffs in your editor.** `sdd review` opens each changed file of the group before ↔ after in VS Code, VSCodium, Cursor or Windsurf (the first found), without blocking the terminal; the right side is the worktree file, so you can edit it there. Choose the editor with `review.editor` in `sdd.yaml` or `SDD_REVIEW_EDITOR`; `none` prints the diff. Without an editor or a graphical session the diff is printed as before. `--no-open` shows only the summary.
- **Expert review of a group** with the new `sdd-review` skill (`/sdd-review G2`, `$sdd-review G2` in Codex): it reviews the group's changes against the criteria they cover, the language profile, the technical spec and the project's guardrails (`CLAUDE.md`, `AGENTS.md`, …), and writes `.sdd/features/<feature>/reviews/G2.md`. In Claude Code it runs in the new `sdd-reviewer` subagent, so your session only receives the summary. It is advice: it never edits code, approves or revises.
- `review_context` takes a `group` (or no arguments) for that brief; `sdd review [<group>] --context` is its CLI form.

### Upgrading a project
- Run `sdd init --agent <your agents>` again to add `sdd-review` (and, for Claude Code, `.claude/agents/sdd-reviewer.md`).
- Codex, Antigravity and generic setups: `AGENTS.md` gained a line on the review brief. `sdd init` keeps your copy; delete it and re-run if you have not edited it.

## v0.1.0 (2026-11-17)

First public release: the four gated phases (specify, plan, implement, validate), brownfield adoption, `sdd merge`, group IDs in `sdd review|approve|revise`, the guard hook, and agent setup for Claude Code (verified end to end), Codex CLI and Antigravity CLI.
