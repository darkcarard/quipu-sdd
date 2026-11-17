# Project Brief
Universal Spec-Driven Development (SDD) CLI Framework

## 1. Project Overview
The **Universal SDD Framework** (`sdd`) is a lightweight, language-agnostic command-line tool that drives software development from explicit, version-controlled specifications. It is installed once as a single Go binary and initialized inside each project. It guides the user and a cloud LLM through four gated phases:

1. **Specify:** write and approve a functional spec, then a technical spec.
2. **Plan:** turn the approved specs into a layered, reviewable task plan.
3. **Implement:** carry out the plan one task group at a time, with a commit and a human review gate after each group.
4. **Validate:** run integration tests and check that specs, plan and code are in sync.

For the full workflow see [WORKFLOW.md](WORKFLOW.md). For spec authoring rules see [SPEC_GUIDELINES.md](SPEC_GUIDELINES.md). For audit and recovery see [TRACEABILITY.md](TRACEABILITY.md). For delivery milestones see [ROADMAP.md](ROADMAP.md).

### High-Level Philosophy
* **Specs are the source of truth.** Code is never generated blindly. Every change traces back to an approved, version-controlled markdown spec through stable requirement IDs.
* **Humans own the gates.** The agent drafts, plans and implements. The user approves each spec, the plan, and each implemented task group.
* **Model agnostic.** Orchestration logic depends only on a neutral `Provider` interface. Vendor HTTP schemas are isolated in adapter packages.
* **Language and tool agnostic.** Tools are reached through the Model Context Protocol (MCP). Language-specific knowledge lives in replaceable *profiles* and in the build and test commands declared by the technical spec.
* **Everything is traceable and resumable.** Every action is journaled. Every step is checkpointed. Work can resume after a crash, a lost connection or a reboot.

## 2. System Constraints & Requirements

### Infrastructure Constraints
* **Cloud LLM APIs only.** The supported providers are Anthropic Claude (MVP), Google Gemini and the OpenAI Responses API (later). Local inference runtimes (Ollama, llama.cpp, etc.) are out of scope.
* **Secure key management.** API keys come only from the process environment or a project-local `.env` file, which must be git-ignored. Keys are never written to the config, the journal, transcripts or logs.

### Architecture Requirements
* **Single binary runtime.** Written in Go with a minimal dependency set: the standard library, a YAML parser and the official MCP Go SDK. Provider adapters use plain `net/http`, not vendor SDKs.
* **Per-project installation.** `sdd init` creates `sdd.yaml`, `specs/` and `.sdd/` inside the project repository. Specs, plans, state and the audit journal are committed alongside the code. `sdd.yaml` pins the `framework_version`.
* **File-based state.** Human-readable markdown and append-only JSONL live under `.sdd/`. Machine state (`state.json`) is always written atomically (temp file + rename).
* **Isolated workspaces.** The Implement phase runs in a dedicated **git worktree** on a private branch `sdd/<feature>`. The user's working branch is never modified until they merge.

## 3. Structural Design

The LLM never talks to tools directly. The orchestrator runs the agent loop:
1. It sends the conversation and the tool definitions to the provider.
2. It receives `tool_use` blocks back.
3. It checks each call against the allowlist and runs it through the MCP hub.
4. It returns the result as a `tool_result`.

```text
                          ┌──────────────────────────────┐
                          │          sdd CLI (Go)        │
                          │  commands · gates · prompts  │
                          └──────────────┬───────────────┘
                                         │
                          ┌──────────────▼───────────────┐
                          │         Orchestrator         │
                          │  workflow state machine,     │
                          │  agent loop, limits, retries │
                          └──┬─────────┬─────────┬───────┘
                             │         │         │
          ┌──────────────────▼──┐  ┌───▼──────┐  ┌▼─────────────────────┐
          │   Provider (neutral)│  │  State   │  │   Tool Hub (MCP)     │
          │  anthropic | gemini │  │ .sdd/    │  │  allowlist, path     │
          │  | openai adapters  │  │ journal, │  │  confinement         │
          └──────────┬──────────┘  │ checkpts │  └─────────┬────────────┘
                     │ HTTPS/SSE   └──────────┘            │ JSON-RPC over stdio
          ┌──────────▼──────────┐                ┌─────────▼────────────┐
          │    Cloud LLM APIs   │                │  MCP servers          │
          │ Claude/Gemini/OpenAI│                │  filesystem, git,     │
          └─────────────────────┘                │  shell/test, docker…  │
                                                 └─────────┬────────────┘
                                                           │
                                                 ┌─────────▼────────────┐
                                                 │ Git worktree          │
                                                 │ .sdd/worktrees/<feat> │
                                                 └──────────────────────┘
```

### Key Modules
1. **Workflow engine.** Owns the per-feature state machine, approval gates and staleness detection based on content hashes.
2. **Orchestrator.** Runs the agent loop for each phase. It enforces iteration, token and time limits and checkpoints after every step.
3. **Provider abstraction.** Neutral request and response types (messages, text, `tool_use`, `tool_result`, usage, stream events). Each adapter maps them to one vendor's HTTP schema and handles that vendor's streaming format and retry semantics.
4. **Tool hub.** An MCP client built on the official Go SDK. It spawns the configured MCP servers as subprocesses over stdio. It exposes their tools to the provider, enforces the allowlist and confines file paths to the active worktree.
5. **State, journal and lock.** `state.json` checkpoints, the append-only `journal.jsonl`, LLM transcripts and per-feature locks.
6. **Workspace.** Creates, repairs and removes git worktrees. Makes checkpoint commits, squashes them per group and writes commit trailers.
7. **Spec, plan and trace.** Parses specs and plans, runs deterministic lint, validates the plan, and builds the ID chain from requirement to test.

## 4. Operational Rules

### Limits
These limits are configurable in `sdd.yaml`:
* Maximum agent iterations per task.
* Maximum fix attempts per group verification.
* Per-request and per-command timeouts.
* An optional token budget per run.

Provider calls retry with exponential backoff and jitter on connection errors and on HTTP 429, 5xx and 529. When retries run out, the run checkpoints and exits with a pointer to `sdd resume`.

### Security
* Only the MCP servers and tools listed in `sdd.yaml` are available to the agent.
* File and shell tools run with the feature worktree as their root. Paths outside it are rejected.
* Destructive actions outside the worktree (push, merge into the user's branch) are never performed by the agent.
* Transcripts redact credentials. The `.sdd/runs/` directory is git-ignored by default.

### Worktree Lifecycle
1. `sdd implement` creates `.sdd/worktrees/<feature>` on branch `sdd/<feature>` from the current `HEAD`.
2. Each finished task gets a WIP checkpoint commit. When the group passes verification, its WIP commits are squashed into one group commit with `SDD-*` trailers.
3. The user reviews after each group: `sdd approve` continues, `sdd revise -m "…"` sends feedback to the agent.
4. When validation is done, the user merges `sdd/<feature>` by their usual process (merge, PR, rebase). `sdd clean <feature>` removes the worktree.
5. After a failure the worktree is kept as-is for inspection and for `sdd resume`.

## 5. MVP Scope
* **Provider:** Anthropic Claude Messages API via `net/http`, with SSE streaming and tool use.
* **Workflow:** all four phases for a single feature at a time.
* **Specs:** `specs/<feature>/functional.md`, `technical.md` and `plan.md`, using the embedded templates.
* **Tools:** MCP filesystem and git servers, plus command execution for the build, lint and test commands declared in the technical spec.
* **Traceability and recovery:** journal, checkpoints, locks, `sdd status`, `sdd resume`, `sdd trace`, `sdd sync check`.

### Out of Scope (MVP)
* Gemini and OpenAI adapters (designed for, delivered in M7).
* Local model inference.
* Delegating work to external agent CLIs (Claude Code, Codex CLI, Gemini CLI).
* Running several features in parallel, remote or shared state, and a web UI.
* Automatic merge or push to remote branches.
