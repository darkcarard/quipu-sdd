# Security Policy

## Reporting a vulnerability

Please report security problems privately, not in a public issue: use **Report a vulnerability** in the repository's **Security** tab (GitHub private vulnerability reporting). Include what you found, how to reproduce it and the `sdd version` output.

You will get an answer within a week. Fixes are released as a new version, and the advisory is published once users can update.

## Scope

`sdd` runs locally and never calls an LLM or any network service. The areas where a problem matters most:

- **Command execution.** `sdd` runs the build, lint, test and integration commands declared in a project's `technical.md` with the user's shell, and its agent hooks run `sdd hook pre-tool-use`. Treat a project's specs like its build scripts: only run `sdd` on projects you trust.
- **The human-only gate.** Approvals, reviews, merges and recovery commands must not be reachable by the agent through the MCP server. A way for an agent to approve through `sdd mcp` is a vulnerability. The installed hooks and command rules are a guard against mistakes, not a sandbox (see [docs/WORKFLOW.md](docs/WORKFLOW.md#agent-setup)).
- **Files outside the project.** `sdd` should only write inside the project (and its git worktrees).

## Getting `sdd` safely

The only official source is <https://github.com/darkcarard/quipu-sdd>. Install with `go install github.com/darkcarard/quipu-sdd/cmd/sdd@<version>` (Go verifies the module against the public checksum database) or build from a clone. Copies distributed elsewhere are not maintained here.
