# CLAUDE.md

Context for Claude Code sessions on this repository, on any machine.

## Project
`sdd` is a single-binary Go CLI for Spec-Driven Development that runs alongside the coding agent the user already has (Claude Code first). It never calls an LLM: the agent reasons, `sdd` owns artifacts, gates, verification, commits and the audit trail. Start with these docs:
- `docs/BRIEF.md`: goals, constraints and architecture
- `docs/WORKFLOW.md`: the four gated phases (specify → plan → implement → validate), the state machine and the commands
- `docs/SPEC_GUIDELINES.md`: spec guardrails, ID conventions (`FR`/`NFR`/`AC`/`ADR`/`G`/`T`) and the plan format
- `docs/TRACEABILITY.md`: audit journal, ID chain, checkpoints and `sdd resume`
- `docs/ROADMAP.md`: milestones and exit criteria

## Current status
- **M0 (scaffold): done.** `sdd init` and `sdd version`.
- **M1 (specs, plans and state): done.** `sdd status`, `sdd spec new [--technical]/lint/approve`, `sdd plan new/approve`, `sdd sync check` (spec↔spec, spec↔plan, approvals) and `sdd trace <ID>|--matrix`. Specs and plans are written by hand for now; the phase is derived from artifacts and approvals (`workflow.Effective`) up to `plan-approved`.
- **M2 (Claude provider): parked.** Built and tested (`internal/provider/...`, `internal/transcript`) for an agent loop inside `sdd`, then set aside when `sdd` moved to running inside the user's agent. Kept for a possible future API mode; nothing imports it from the CLI.
- **M3 (agent integration + Specify): done.** `sdd mcp` (stdio, official Go SDK) with `status`, `guide`, `spec_new`, `plan_new`, `lint`, `sync_check`, `trace`; `sdd guide`; `sdd init --agent claude|generic` and `--check` (never modifies existing files; ephemeral paths in `.sdd/.gitignore`); Claude Code skills `/sdd-spec`, `/sdd-tech`; agents journaled as `agent:<client>` and unable to approve (`feature.ErrHumanOnly`); commands find the project root from any subdirectory.
- **M4 (Plan): done.** `guide plan` with a coverage checklist built from the approved specs (IDs with their components/test levels, file limit, Architecture, language), `/sdd-plan` skill, plan layer check against the technical Architecture (`plan.CheckLayers`, warning). Next-step texts point to the agent commands and say approvals are the user's.
- **M5 (Implement): done.** `internal/workspace` (git CLI: worktree ensure/repair, checkpoints, squash with trailers, diff, reset, log), `internal/verify` (technical-spec commands with timeout, process-group kill, output tail), `internal/implement` (group/task state machine: `Start`, `TaskNext`, `TaskDone`, `GroupVerify`, `ReviewGroup`, `ApproveGroup`, `ReviseGroup`, `Resume`, `Clean`). CLI `implement`, `task next|done`, `group verify`, `review`, `approve`, `revise`, `resume`, `clean`; MCP `task_next`, `task_done`, `group_verify`; `/sdd-implement`; `guide implement`. After an approval the agent's `task_next` starts the next group. `FindRoot` maps a worktree to its owning project.
- **Next: M6.** Validate: `sdd validate`, plan↔code sync (group commits and trailers, `covers: AC-###` in tests), full `trace` with commits and tests, `report.md`, `review_context` and `/sdd-deep-review`. Brownfield adoption is M7.

Update this section whenever a milestone is completed.

## Decisions (do not revisit without asking)
- **`sdd` never calls an LLM.** The reasoning comes from the host coding agent (Claude Code first; Codex CLI, Gemini CLI and `AGENTS.md` readers in M8), under the user's subscription. `sdd` is deterministic: everything a gate depends on (lint, coverage, staleness, build and test results, trailers) is checked by `sdd`, never left to the agent.
- The agent reaches `sdd` through `sdd mcp` (stdio MCP server, official Go SDK `github.com/modelcontextprotocol/go-sdk`) or the CLI. Both are thin layers over the same use cases (`internal/feature` and successors). Per-agent knowledge lives only in the files `sdd init --agent` installs.
- Approvals and group review decisions are human-only: never MCP tools, and the installed permission rules deny the agent those commands.
- The own agent loop ("API mode") is deferred. `internal/provider`, `provider/anthropic`, `provider/registry` and `internal/transcript` stay in the tree, unused; do not extend or delete them without asking. If API mode is revived, its decisions apply: plain `net/http` adapters, no local models.
- The binary is installed once; each project is set up with `sdd init`. Specs, the plan, state and the journal are committed inside that project.
- `sdd init` never modifies an existing file (not even `.gitignore`: ephemeral paths go in `.sdd/.gitignore`). It creates what is missing and prints snippets for the rest. Existing projects are adopted as baseline specs without touching their code.
- Keep dependencies minimal: stdlib plus `gopkg.in/yaml.v3`, and the official MCP Go SDK from M3.
- (API mode, parked) Anthropic requests use API version `2026-10-21` (no `temperature`/`top_p`/`top_k`), adaptive thinking with explicit `effort`, automatic prompt caching and, on supported models, server-side refusal `fallbacks`. Conversations are append-only: assistant blocks keep their wire form in `ContentBlock.Raw` and are replayed byte for byte.

## Conventions
- All docs, templates, code comments and CLI messages are in **English**. The user converses in **Spanish**.
- Follow `internal/templates/profiles/go.md`: gofmt, table-driven tests, `t.TempDir()`, wrapped errors, `context.Context` first.
- When a change affects behavior described in `docs/`, update the docs in the same commit.
- Commit once per milestone (or per coherent step), then push to `origin` (github.com/darkcarard/sdd-framework). The user works from several machines.

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
