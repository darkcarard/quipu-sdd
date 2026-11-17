# CLAUDE.md

Context for Claude Code sessions on this repository, on any machine.

## Project
**Quipu** is the tool's name; its command is `sdd` (Spec-Driven Development, the method). Keep `sdd` for the binary, `.sdd/`, `sdd.yaml`, the `/sdd-*` commands, the `sdd` MCP server and the `SDD-*` trailers; use "Quipu" for the product in docs and prose. Repository: github.com/darkcarard/quipu-sdd.

`sdd` is a single-binary Go CLI for Spec-Driven Development that runs alongside the coding agent the user already has (Claude Code first). It never calls an LLM: the agent reasons, `sdd` owns artifacts, gates, verification, commits and the audit trail. Start with these docs:
- `docs/BRIEF.md`: goals, constraints and architecture
- `docs/WORKFLOW.md`: the four gated phases (specify → plan → implement → validate), the state machine and the commands
- `docs/SPEC_GUIDELINES.md`: spec guardrails, ID conventions (`FR`/`NFR`/`AC`/`ADR`/`G`/`T`) and the plan format
- `docs/TRACEABILITY.md`: audit journal, ID chain, checkpoints and `sdd resume`
- `docs/ROADMAP.md`: milestones and exit criteria
- `docs/E2E_TEST.md`: manual end-to-end test in a real Claude Code session
- `docs/MANUAL.html` / `docs/MANUAL.es.html`: illustrated user manual (English / Spanish, kept in sync; update both when commands or the workflow change)

## Current status
- **M0 (scaffold): done.** `sdd init` and `sdd version`.
- **M1 (specs, plans and state): done.** `sdd status`, `sdd spec new [--technical]/lint/approve`, `sdd plan new/approve`, `sdd sync check` (spec↔spec, spec↔plan, approvals) and `sdd trace <ID>|--matrix`. Specs and plans are written by hand for now; the phase is derived from artifacts and approvals (`workflow.Effective`) up to `plan-approved`.
- **M2 (Claude provider): parked.** Built and tested (`internal/provider/...`, `internal/transcript`) for an agent loop inside `sdd`, then set aside when `sdd` moved to running inside the user's agent. Kept for a possible future API mode; nothing imports it from the CLI.
- **M3 (agent integration + Specify): done.** `sdd mcp` (stdio, official Go SDK) with `status`, `guide`, `spec_new`, `plan_new`, `lint`, `sync_check`, `trace`; `sdd guide`; `sdd init --agent claude|generic` and `--check` (never modifies existing files; ephemeral paths in `.sdd/.gitignore`); Claude Code skills `/sdd-spec`, `/sdd-tech`; agents journaled as `agent:<client>` and unable to approve (`feature.ErrHumanOnly`); commands find the project root from any subdirectory.
- **M4 (Plan): done.** `guide plan` with a coverage checklist built from the approved specs (IDs with their components/test levels, file limit, Architecture, language), `/sdd-plan` skill, plan layer check against the technical Architecture (`plan.CheckLayers`, warning). Next-step texts point to the agent commands and say approvals are the user's.
- **M5 (Implement): done.** `internal/workspace` (git CLI: worktree ensure/repair, checkpoints, squash with trailers, diff, reset, log), `internal/verify` (technical-spec commands with timeout, process-group kill, output tail), `internal/implement` (group/task state machine: `Start`, `TaskNext`, `TaskDone`, `GroupVerify`, `ReviewGroup`, `ApproveGroup`, `ReviseGroup`, `Resume`, `Clean`). CLI `implement`, `task next|done`, `group verify`, `review`, `approve`, `revise`, `resume`, `clean`; MCP `task_next`, `task_done`, `group_verify`; `/sdd-implement`; `guide implement`. After an approval the agent's `task_next` starts the next group. `FindRoot` maps a worktree to its owning project.
- **M6 (Validate): done.** `internal/validate`: `CheckCode` (one commit per group with matching trailers on `sdd/<feature>`, commits without trailers, `covers:` references found with `git grep`, every AC of an approved group tested), `Index` (trace with commits and tests), `Validate` (integration in the worktree, all sync checks, `report.md`, phase `validated`), `ReviewContext`. CLI `validate`, `review --context`; `sync check` and `trace` include code; MCP `review_context`; `/sdd-deep-review`.
- **Token economy of agent-facing output: done.** Profile sent once per group (and on resume), one-line rules in `task_next`, `limits.output_tail` (6000 chars) for failing command output, guides omit the template once the file exists, shorter MCP tool descriptions. Measured: later `task_next` ~700→~230 tokens, tool catalog ~1590→~1160, worst failure output ~4000→~1500.
- **M7 (Brownfield adoption): done.** `internal/adopt` (deterministic, compact inventory: languages, build files, suggested commands, layout, tests, docs), baseline features (`origin: existing` in functional frontmatter and state; `workflow.EffectiveFor` → `baselined`; no plan: `feature.ErrBaselinePlan`), `sdd adopt --context` / MCP `adopt_context`, `spec new --baseline` / `spec_new baseline`, `guide adopt`, `/sdd-adopt`; trace shows baselines as existing code and links tests that reference them; sync warns about unapproved dependencies.
- **E2E feedback (first real Claude Code run): done.** Every spec/plan approval commits `specs/<f>/` and `.sdd/features/<f>/` (only those paths, `workspace.Git.CommitPaths`; a failure is a note, never a failed approval). `sdd merge [<f>]` (human-only, denied to the agent) merges `sdd/<f>` into the project checkout's branch from any directory (ff, else merge commit, conflicts aborted), refuses if the branch moved since validation (`state.validated_sha`), records a merge already done outside sdd (validated commit already in HEAD, even with the branch deleted) without merging, removes the worktree, records `state.merged` and commits the records; `sdd clean` with no worktree is a no-op. `sdd init --agent claude` also installs a `PreToolUse` hook (`sdd hook pre-tool-use`, `agents.Guard`) that denies human-only commands anywhere in a Bash command (the E2E run showed `bash -c "sdd approve …"` passing the deny rules); env vars cannot tell the user's `!` from the agent (both carry `CLAUDECODE=1`/`AI_AGENT`, no TTY). `review`/`approve`/`revise` take a group ID (`sdd approve G2`) or nothing; Next texts use group IDs and "type `/sdd-implement <f>` in Claude Code".
- **Next: M8.** Integration files for Codex CLI and Gemini CLI. M8 also decouples the docs from Claude Code: today the manuals, the README quick start, `WORKFLOW.md` "Agent setup" and `E2E_TEST.md` assume Claude Code. Make them agent-neutral ("in your agent", one setup subsection per agent, an agent selector in the manuals' Set up tab, a per-agent section in `E2E_TEST.md`) once each agent's commands and permissions are verified. The first end-to-end run in Claude Code (`docs/E2E_TEST.md`) passed, including the safety checks: the hook denies human-only commands to the agent in any form, and `!` commands are not affected.

Update this section whenever a milestone is completed.

## Decisions (do not revisit without asking)
- **`sdd` never calls an LLM.** The reasoning comes from the host coding agent (Claude Code first; Codex CLI, Gemini CLI and `AGENTS.md` readers in M8), under the user's subscription. `sdd` is deterministic: everything a gate depends on (lint, coverage, staleness, build and test results, trailers) is checked by `sdd`, never left to the agent.
- The agent reaches `sdd` through `sdd mcp` (stdio MCP server, official Go SDK `github.com/modelcontextprotocol/go-sdk`) or the CLI. Both are thin layers over the same use cases (`internal/feature` and successors). Per-agent knowledge lives only in the files `sdd init --agent` installs.
- Approvals and group review decisions are human-only: never MCP tools, and the installed permission rules and PreToolUse hook deny the agent those commands.
- The own agent loop ("API mode") is deferred. `internal/provider`, `provider/anthropic`, `provider/registry` and `internal/transcript` stay in the tree, unused; do not extend or delete them without asking. If API mode is revived, its decisions apply: plain `net/http` adapters, no local models.
- The binary is installed once; each project is set up with `sdd init`. Specs, the plan, state and the journal are committed inside that project.
- `sdd init` never modifies an existing file (not even `.gitignore`: ephemeral paths go in `.sdd/.gitignore`). It creates what is missing and prints snippets for the rest. Existing projects are adopted as baseline specs without touching their code.
- Keep dependencies minimal: stdlib plus `gopkg.in/yaml.v3`, and the official MCP Go SDK from M3.
- Agent-facing output (MCP tool results, guides, tool descriptions, skills) is paid for in the agent's tokens: keep it compact, send large context once, and measure before adding to it.
- (API mode, parked) Anthropic requests use API version `2026-10-21` (no `temperature`/`top_p`/`top_k`), adaptive thinking with explicit `effort`, automatic prompt caching and, on supported models, server-side refusal `fallbacks`. Conversations are append-only: assistant blocks keep their wire form in `ContentBlock.Raw` and are replayed byte for byte.

## Conventions
- All docs, templates, code comments and CLI messages are in **English**. The user converses in **Spanish**.
- Follow `internal/templates/profiles/go.md`: gofmt, table-driven tests, `t.TempDir()`, wrapped errors, `context.Context` first.
- When a change affects behavior described in `docs/`, update the docs in the same commit.
- Commit once per milestone (or per coherent step), then push to `origin` (github.com/darkcarard/quipu-sdd). The user works from several machines.

## Commands
```sh
make check   # gofmt check + go vet + go test -race (must pass before committing)
make build   # bin/sdd
make install # go install with version ldflags
```

## Layout
- `cmd/sdd`: entry point and signal handling
- `internal/cli`: the command dispatcher, `init` and thin command wrappers over `internal/app`
- `internal/app`: use cases shared by the CLI and the MCP server (results as structs, text formatters, `guide` with the embedded agent guides in `app/guides/`)
- `internal/mcpserver`: `sdd mcp`; human-only operations are never registered as tools
- `internal/agents`: integration files per agent (`files/claude`, `files/generic`) and the never-overwrite installer
- `internal/implement`, `internal/workspace`, `internal/verify`: Phase 3 (see M5 above)
- `internal/validate`: Phase 4 (see M6 above)
- `internal/adopt`: brownfield inventory (see M7 above)
- `internal/config`: configuration (`sdd.yaml` plus `.env` and environment variables)
- `internal/workflow`: phases, transitions, `Evaluate` (staleness), `Effective` (derived phase)
- `internal/state`: `Layout` paths, atomic writes, `FeatureState`
- `internal/journal`: append-only JSONL with start/end pairing
- `internal/lock`: per-feature lock
- `internal/templates`: embedded templates and profiles
- `internal/markdown`: line-oriented reader (frontmatter, headings, bullets, tables) with line numbers; `SetFrontmatter` stamps hashes
- `internal/diag`: lint/sync diagnostics (`file:line: severity: message`)
- `internal/spec`: functional/technical parsers, lint, `CheckTechnical` (spec↔spec)
- `internal/plan`: plan parser, `Validate` (structure), `CheckCoverage` (spec↔plan)
- `internal/trace`: ID index over specs and plan, tree rendering and matrix
- `internal/feature`: use cases on one feature (load, lint, sync, create from template, approve, next action); the CLI and later the orchestrator call it
- `internal/sddtest`: a valid fixture feature shared by tests
- Parked for API mode (unused): `internal/provider` (+ `anthropic`, `registry`), `internal/transcript`, `internal/tools`, `internal/orchestrator`
