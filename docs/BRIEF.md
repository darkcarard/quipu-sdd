# Project Brief
Universal Spec-Driven Development (SDD) CLI Framework

## 1. Project Overview
The **Universal SDD Framework** (`sdd`) is a lightweight, language-agnostic command-line tool that drives software development from explicit, version-controlled specifications. It is installed once as a single Go binary and initialized inside each project.

`sdd` does not call any LLM itself. The reasoning comes from the **coding agent the user already runs**, such as Claude Code, Codex CLI or Gemini CLI, under the user's existing subscription. `sdd` is the deterministic engine around that agent: it owns the artifacts, the guardrails, the approval gates, the state, the audit trail, the isolated worktree, verification and commits. The agent reaches `sdd` through a local MCP server and through commands installed into the project.

Together they move a feature through four gated phases:

1. **Specify:** the agent drafts a functional spec, then a technical spec, with the user. The user approves each one.
2. **Plan:** the agent turns the approved specs into a layered, reviewable task plan. The user approves it.
3. **Implement:** the agent implements the plan one task at a time in an isolated git worktree. `sdd` verifies each task and group, makes the commits, and stops for a human review after each group.
4. **Validate:** `sdd` runs the integration tests and checks that specs, plan and code are in sync.

For the full workflow see [WORKFLOW.md](WORKFLOW.md). For spec authoring rules see [SPEC_GUIDELINES.md](SPEC_GUIDELINES.md). For audit and recovery see [TRACEABILITY.md](TRACEABILITY.md). For delivery milestones see [ROADMAP.md](ROADMAP.md).

### High-Level Philosophy
* **Specs are the source of truth.** Code is never generated blindly. Every change traces back to an approved, version-controlled markdown spec through stable requirement IDs.
* **Humans own the gates.** The agent drafts, plans and implements. The user approves each spec, the plan and each implemented task group. Approvals are not available to the agent.
* **The agent reasons, `sdd` decides.** Everything that must be true for the workflow to advance (lint, coverage, staleness, build and test results, commit trailers) is checked deterministically by `sdd`, never left to the agent's judgment.
* **Agent agnostic.** Any agent that speaks MCP or can run shell commands can drive `sdd`. Claude Code is supported first; Codex CLI, Gemini CLI and agents that read `AGENTS.md` follow. Agent-specific knowledge lives in thin instruction files.
* **Language agnostic.** Language-specific knowledge lives in replaceable *profiles* and in the build and test commands declared by the technical spec.
* **Everything is traceable and resumable.** Every action is journaled and every step is checkpointed. If the agent session ends, a new session continues from the recorded state.

## 2. System Constraints & Requirements

### Infrastructure Constraints
* **No LLM calls in `sdd`.** No API keys, no provider SDKs, no model configuration. The agent runtime, its model, its billing and its conversation history belong to the host agent.
* **Local only.** `sdd` runs on the developer's machine against the local repository. It needs no network access.

### Architecture Requirements
* **Single binary runtime.** Written in Go with a minimal dependency set: the standard library, a YAML parser and the official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`) for the MCP server.
* **Per-project installation.** `sdd init` creates `sdd.yaml`, `specs/` and `.sdd/` inside the project repository and, with `--agent`, installs that agent's integration files. Specs, plans, state and the audit journal are committed alongside the code. `sdd.yaml` pins the `framework_version`.
* **Non-intrusive.** `sdd init` never modifies an existing file: it only creates missing ones and prints the snippet to add by hand for the rest. Existing projects are adopted by documenting them as baseline specs, without changing their code.
* **File-based state.** Human-readable markdown and append-only JSONL live under `.sdd/`. Machine state (`state.json`) is always written atomically (temp file + rename).
* **Isolated workspaces.** The Implement phase runs in a dedicated **git worktree** on a private branch `sdd/<feature>`. `sdd` never modifies the user's working branch; the user merges when they are ready.
* **One engine, two surfaces.** The CLI and the MCP server are thin layers over the same use cases (`internal/feature` and its successors), so both behave identically.

## 3. Structural Design

```text
   ┌──────────────────────────────┐            ┌──────────────────────────┐
   │  Host coding agent           │            │  User (terminal)         │
   │  Claude Code · Codex CLI ·   │            │  approvals, reviews,     │
   │  Gemini CLI · …              │            │  status, trace           │
   │  /sdd-* commands, AGENTS.md  │            └────────────┬─────────────┘
   └──────────────┬───────────────┘                         │ sdd <command>
                  │ MCP over stdio (or shell commands)      │
   ┌──────────────▼──────────────────────────────────────────▼─────────────┐
   │                          sdd (Go, single binary)                       │
   │  MCP server  ·  CLI                                                    │
   │  ┌──────────────────────────────────────────────────────────────────┐  │
   │  │ Use cases: spec/plan drafting support, lint, sync, approvals,    │  │
   │  │ task hand-off, verification, commits, review, validate, trace    │  │
   │  └───────┬──────────────┬───────────────┬──────────────┬────────────┘  │
   │   workflow/state   spec · plan · trace  workspace (git)  journal · lock │
   └───────────┬──────────────────────────────────────┬─────────────────────┘
               │                                      │
       specs/<feature>/, .sdd/                .sdd/worktrees/<feature>
       (committed)                            (branch sdd/<feature>)
```

### Key Modules
1. **Workflow engine.** The per-feature state machine, approval gates and staleness detection based on content hashes.
2. **Spec, plan and trace.** Parsers, deterministic lint, plan validation and the ID chain from requirement to test.
3. **Agent hand-off.** Builds what the agent needs at each step (guidelines, templates, approved specs, the current task with its spec excerpts and the language profile) and records what the agent reports back.
4. **MCP server.** `sdd mcp` serves the agent-facing operations over stdio with the official Go SDK. Human-only operations (approvals, group review decisions) are not exposed.
5. **Agent integrations.** `sdd init --agent <name>` installs the instruction files, slash commands, MCP registration and permission rules for one host agent, creating only files that do not exist.
8. **Adoption.** Inventories an existing project and supports the agent in documenting it as baseline features, so brownfield projects can use the workflow without changes to their code.
6. **Workspace.** Creates, repairs and removes git worktrees. Runs the verification commands, makes checkpoint commits, squashes them per group and writes commit trailers.
7. **State, journal and lock.** `state.json` checkpoints, the append-only `journal.jsonl` and per-feature locks.

## 4. Operational Rules

### Limits
These limits are configurable in `sdd.yaml`:
* Maximum files per task (plan validation).
* Maximum fix attempts per group verification.
* Timeout for each verification command.
* How much of a failing command's output is returned to the agent (`output_tail`).

Agent-facing output is kept compact on purpose, because it is read by the agent and costs tokens: guides leave out templates once the file exists, the language profile is sent once per group, and failure output is cut to its tail.

### Security and Enforcement
`sdd` cannot sandbox the host agent: file access, shell access and network access are governed by the agent's own permission system. What `sdd` guarantees deterministically:
* A gate only opens when its checks pass: lint and sync for approvals, build and tests for task and group completion.
* Approvals and review decisions are human-only, and so are `sdd resume` and `sdd clean`, which can discard work. They are not MCP tools, the installed permission rules deny the agent the matching shell commands, and the core rejects them when the caller is an agent. Every approval records the git identity of the approver.
* Commits on `sdd/<feature>` are made by `sdd`, with trailers. Changes outside the current task's declared files are reported at task completion and in the group review.
* `sdd` never pushes and never merges into the user's branch.

### Worktree Lifecycle
1. Starting the Implement phase creates `.sdd/worktrees/<feature>` on branch `sdd/<feature>` from the current `HEAD`.
2. The agent edits files in the worktree. When it reports a task done, `sdd` verifies it and makes a WIP checkpoint commit. When the group passes verification, its WIP commits are squashed into one group commit with `SDD-*` trailers.
3. The user reviews after each group: `sdd approve` continues, `sdd revise -m "…"` records feedback for the agent to address.
4. When validation is done, the user merges `sdd/<feature>` by their usual process (merge, PR, rebase). `sdd clean <feature>` removes the worktree.
5. After a failure the worktree is kept as-is for inspection and for `sdd resume`.

## 5. MVP Scope
* **Agent:** Claude Code (MCP server registration, slash commands, permission rules), plus a generic `AGENTS.md` section usable by other agents.
* **Workflow:** all four phases for a single feature at a time, on a new (greenfield) project.
* **Specs:** `specs/<feature>/functional.md`, `technical.md` and `plan.md`, using the embedded templates.
* **Verification:** the build, lint, test and integration commands declared in the technical spec.
* **Traceability and recovery:** journal, checkpoints, locks, `sdd status`, `sdd resume`, `sdd trace`, `sdd sync check`.

### Out of Scope (MVP)
* Adoption of existing (brownfield) projects (M7).
* Integration files for agents other than Claude Code (M8).
* **API mode:** an agent loop inside `sdd` that calls LLM APIs directly, for unattended runs. A Claude provider was built for it (`internal/provider`, `internal/transcript`) and is kept, unused, for a possible future version.
* Local model inference.
* Running several features in parallel, remote or shared state, and a web UI.
* Automatic merge or push to remote branches.
