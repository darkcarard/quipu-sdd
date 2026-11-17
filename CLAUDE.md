# CLAUDE.md

Context for Claude Code sessions on this repository, on any machine.

## Project
`sdd` is a single-binary Go CLI for Spec-Driven Development that works with any LLM API. Start with these docs:
- `docs/BRIEF.md`: goals, constraints and architecture
- `docs/WORKFLOW.md`: the four gated phases (specify → plan → implement → validate), the state machine and the commands
- `docs/SPEC_GUIDELINES.md`: spec guardrails, ID conventions (`FR`/`NFR`/`AC`/`ADR`/`G`/`T`) and the plan format
- `docs/TRACEABILITY.md`: audit journal, ID chain, checkpoints and `sdd resume`
- `docs/ROADMAP.md`: milestones and exit criteria

## Current status
- **M0 (scaffold): done.** `sdd init` and `sdd version` work; the other commands are stubs that name their milestone.
- **Next: M1.** Spec and plan parsers, deterministic lint, the workflow state machine wired to commands, `sdd status`, `sdd spec new/lint/approve`, `sdd plan approve`, staleness detection, `sync check` (spec↔spec, spec↔plan) and `trace` over specs and plan.

Update this section whenever a milestone is completed.

## Decisions (do not revisit without asking)
- The framework is agnostic across **cloud LLM APIs only** (Claude first, then Gemini and OpenAI) behind the neutral `internal/provider.Provider` interface. No delegation to external agent CLIs and no local models.
- Provider adapters use plain `net/http`, not vendor SDKs.
- MCP goes through the official Go SDK (`github.com/modelcontextprotocol/go-sdk`), arriving in M5.
- The binary is installed once; each project is set up with `sdd init`. Specs, the plan, state and the journal are committed inside that project.
- The orchestrator depends only on interfaces (`provider`, `tools`, `workspace`).
- Keep dependencies minimal: stdlib plus `gopkg.in/yaml.v3` so far.

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
- `internal/cli`: the command dispatcher and `init`
- `internal/config`: configuration (`sdd.yaml` plus `.env` and environment variables)
- `internal/workflow`: phases, transitions, `Evaluate` (staleness)
- `internal/state`: `Layout` paths, atomic writes, `FeatureState`
- `internal/journal`: append-only JSONL with start/end pairing
- `internal/lock`: per-feature lock
- `internal/templates`: embedded templates and profiles
- `internal/spec`, `internal/plan`, `internal/trace`: types only (M1)
- `internal/provider`, `internal/provider/anthropic`, `internal/tools`, `internal/workspace`, `internal/orchestrator`: interfaces and stubs
