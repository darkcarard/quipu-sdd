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
- **M0 (scaffold): done.** `sdd init` and `sdd version`.
- **M1 (specs, plans and state): done.** `sdd status`, `sdd spec new [--technical]/lint/approve`, `sdd plan new/approve`, `sdd sync check` (spec↔spec, spec↔plan, approvals) and `sdd trace <ID>|--matrix`. Specs and plans are written by hand for now; the phase is derived from artifacts and approvals (`workflow.Effective`) up to `plan-approved`.
- **Next: M2.** Anthropic provider over `net/http`: SSE streaming, tool use, retries and backoff, usage accounting, redacted transcripts, contract tests with recorded fixtures.

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
- `internal/cli`: the command dispatcher, `init` and thin command wrappers over `internal/feature`
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
- `internal/provider`, `internal/provider/anthropic`, `internal/tools`, `internal/workspace`, `internal/orchestrator`: interfaces and stubs
