# Changelog

All notable changes to Quipu (`sdd`). Versions follow [semantic versioning](https://semver.org); before 1.0, a minor version can add features and change behavior.

To update: `go install github.com/darkcarard/quipu-sdd/cmd/sdd@latest`, then `sdd init --agent <your agents>` in each project to add the new integration files (see the README).

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
