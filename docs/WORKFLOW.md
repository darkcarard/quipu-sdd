# SDD Workflow

Every unit of work is a **feature**. Each feature lives in `specs/<feature>/` (artifacts) and `.sdd/features/<feature>/` (state and audit). A feature moves through four phases, and a human approval gate separates each one from the next.

```text
 Phase 1: Specify            Phase 2: Plan        Phase 3: Implement             Phase 4: Validate
┌────────────────────────┐  ┌──────────────┐  ┌──────────────────────────────┐  ┌──────────────────┐
│ functional ⇄ user      │  │ agent drafts │  │ per group G1..Gn:            │  │ integration tests│
│   ✔ approve functional │─▶│ plan.md      │─▶│  tasks → build/lint/test →   │─▶│ sync check       │
│ technical ⇄ user       │  │ ✔ approve    │  │  commit → ✔ review gate      │  │ report.md        │
│   ✔ approve technical  │  │   plan       │  │                              │  │                  │
└────────────────────────┘  └──────────────┘  └──────────────────────────────┘  └──────────────────┘
          ▲                         ▲                         ▲
          └───────── sdd sync check / sdd trace / sdd status (available at every phase) ─────────┘
```

## Who Does What

The LLM work is done by the **host agent** the user already runs (Claude Code first; see [BRIEF.md](BRIEF.md)). `sdd` never calls a model. The split is the same in every phase:

| Role | Does | Through |
|---|---|---|
| **Agent** | Interviews the user, drafts and edits specs and the plan, implements tasks, fixes failures | The `sdd` MCP server (or the CLI from its shell) and the installed `/sdd-*` commands |
| **`sdd`** | Creates artifacts from templates, lints, checks sync and coverage, hands each task to the agent, verifies, commits, journals | Its own code, deterministically |
| **User** | Answers the agent, approves specs and the plan, reviews each group | The `sdd` CLI in their terminal (in Claude Code, `! sdd …` also works) |

Approval and review commands are human-only: they are not exposed as MCP tools, and the permission rules installed by `sdd init --agent` deny them to the agent.

### Agent setup

**`sdd init` never modifies a file that already exists.** It creates the files that are missing; for each one that exists, it leaves it untouched and prints the snippet to add by hand (and `sdd init --check` reports what is still missing). Its own ephemeral paths are ignored through `.sdd/.gitignore`, so the project's `.gitignore` is not touched either.

`sdd init --agent claude` sets up Claude Code:
- `.mcp.json`: registers `sdd mcp` (stdio) as the `sdd` MCP server. Claude Code asks once to approve a project's MCP servers.
- `.claude/skills/sdd-*/SKILL.md`: the `/sdd-*` commands below (Claude Code skills). Each one carries its own instructions, and the MCP server sends the workflow rules to the agent when it connects, so no change to `CLAUDE.md` is needed.
- `.claude/settings.json`: `allow` for every `sdd` MCP tool (`mcp__sdd__*`) and `deny` rules for the approval and review commands (`Bash(sdd spec approve *)` and the like). Deny rules take precedence over any allow rule. They match the command as typed, so they are a guard against mistakes, not a sandbox; the approvals' git identity and the journal record who approved.

`sdd init --agent generic` writes an `AGENTS.md` that explains the workflow to any agent that reads it, if the project has none. Integration files for more agents arrive in M8.

| Slash command | Starts | Available |
|---|---|---|
| `/sdd-spec <feature or "Free text">` | Drafting the functional spec (creates the feature if needed) | M3 |
| `/sdd-tech <feature>` | Drafting the technical spec | M3 |
| `/sdd-plan <feature>` | Drafting the plan | M4 |
| `/sdd-implement <feature>` | Implementing tasks until the current group is ready for review | M5 |
| `/sdd-deep-review <feature>` | A semantic review of the code against the acceptance criteria | M6 |
| `/sdd-adopt` | Documenting an existing project as baseline specs (see [Adopting an existing project](#adopting-an-existing-project-brownfield)) | M7 |

Re-running `sdd init --agent claude` after upgrading `sdd` adds the commands of new milestones; existing ones are left as they are.

### MCP tools

`sdd mcp` serves these tools; it finds the project from its working directory (the nearest `sdd.yaml` at or above it). Each tool calls the same code as the CLI command next to it and returns the same text. State changes made through MCP are journaled as `agent:<client>`. `status`, `guide`, `spec_new`, `plan_new`, `lint`, `sync_check` and `trace` are available from M3; the others arrive with the phase that needs them.

| Tool | Purpose | CLI equivalent |
|---|---|---|
| `status` | Phase, artifact statuses and the next action | `sdd status` |
| `guide` | Instructions, format rules and template for `workflow`, `functional`, `technical` and `plan` (`implement` in M5), plus the status of a given feature and, for `plan`, its coverage checklist | `sdd guide <topic> [--feature <feature>]` |
| `spec_new` | Create a feature and its functional spec, or its technical spec | `sdd spec new` |
| `plan_new` | Create `plan.md` from the template, with the source hashes stamped | `sdd plan new` |
| `lint` | Lint the feature's artifacts (plan validation included) | `sdd spec lint` |
| `sync_check` | spec↔spec and spec↔plan consistency | `sdd sync check` |
| `trace` | Follow an ID through the chain | `sdd trace` |
| `task_next` | Start (or resume) the next task, or return pending review feedback | `sdd task next` |
| `task_done` | Verify the current task and checkpoint it | `sdd task done` |
| `group_verify` | Verify the whole group and squash it into one commit | `sdd group verify` |
| `review_context` | Specs, plan, commits and test references for a deep review | `sdd review --context` |
| `adopt_context` | An inventory of an existing project for `/sdd-adopt` | `sdd adopt --context` |

## State Machine

The feature state is stored in `.sdd/features/<feature>/state.json`:

| State | Meaning | Leaves via |
|---|---|---|
| `functional-drafting` | Functional spec being written with the agent | `sdd spec approve --functional` |
| `functional-approved` | Functional spec frozen at a hash | technical spec created (`/sdd-tech` or `sdd spec new --technical`) |
| `technical-drafting` | Technical spec being written | `sdd spec approve --technical` |
| `technical-approved` | Both specs frozen | plan created (`/sdd-plan` or `sdd plan new`) |
| `plan-drafting` | Plan written, awaiting review | `sdd plan approve` |
| `plan-approved` | Plan frozen | `sdd implement` |
| `implementing` | Groups being executed (see group states) | last group approved |
| `implemented` | All groups committed and approved | `sdd validate` passes |
| `validated` | Integration tests and sync checks pass | — |
| `baselined` | Baseline feature: both specs approved, describing code that already exists (no plan, no implementation) | a revision of the baseline specs |

Up to `plan-approved`, the phase is **derived from the artifacts**: their existence, the approvals in `state.json` and the current content hashes (see the staleness rule below). Editing a file by hand or pulling a teammate's changes is therefore always reflected by `sdd status`, and an edited or stale artifact reopens its `*-drafting` phase. From `implementing` onwards, the phase stored in `state.json` is kept as long as every artifact is still approved. The stored phase is updated on the next command that writes state, such as an approval.

**Group states:** `pending → in-progress → awaiting-review → approved`. A `sdd revise` request sends a group from `awaiting-review` back to `in-progress`.

**Task states:** `pending → in-progress → done`, plus `failed` and `interrupted`.

### Staleness Rule
Each approval stores the SHA-256 of the approved artifact. Each downstream artifact stores the hashes of its sources:
- `technical.md` stores `functional_hash`.
- `plan.md` stores `functional_hash` and `technical_hash`.

These source hashes are **stamped automatically** into the frontmatter when the downstream artifact is created and again when it is approved, and the approval in `state.json` records them as well. Hashes normalize CRLF to LF, so a checkout with `core.autocrlf` does not look like an edit.

If a source changes after approval, every artifact that depends on it becomes **stale**. The feature cannot move forward until the stale artifacts are reconciled and approved again. For example, editing an approved functional spec makes both the technical spec and the plan stale.

## Phase 1: Specify (agent + user, iterative)

| Command | Who | Purpose |
|---|---|---|
| `/sdd-spec <feature or "Free text">` | user → agent | Draft the functional spec with the agent |
| `sdd spec new <feature or "Free text"> [--name <feature>] [--title "<title>"] [--description "<text>"]` | user or agent | Create `specs/<feature>/functional.md` from the template and record the feature name, title and description (see [feature names](SPEC_GUIDELINES.md#project-layout)) |
| `sdd spec lint [<feature>]` | user or agent | Deterministic guardrail checks (see [SPEC_GUIDELINES.md](SPEC_GUIDELINES.md)) |
| `sdd spec approve <feature> --functional` | **user** | Freeze the functional spec (lint must pass) |
| `/sdd-tech <feature>` | user → agent | Draft the technical spec. Only allowed after functional approval |
| `sdd spec new <feature> --technical` | user or agent | Create `technical.md` from the template |
| `sdd spec approve <feature> --technical` | **user** | Freeze the technical spec (lint + sync must pass) |

The drafting loop:
1. The agent calls `guide` for the guidelines and the template, and `spec_new` if the artifact does not exist yet.
2. It interviews the user, drafts or updates sections in the file, and asks clarifying questions.
3. After every edit it calls `lint` and fixes what it can; it raises what needs a decision as a question, and keeps Open Questions in the spec until they are answered.
4. When lint passes and the user is satisfied, the agent asks the user to run the approval command. It never approves.

The agent edits only the spec being drafted. Its conversation lives in the host agent's own session history; `sdd` journals the artifacts' creation and approval, and the state lets a new session pick up where the last one stopped.

Ordering is strict: functional first, then technical. The technical spec must map every `FR-###` and `NFR-###` to at least one component, and every `AC-###` to a test type.

## Phase 2: Plan (agent drafts, user approves)

| Command | Who | Purpose |
|---|---|---|
| `/sdd-plan <feature>` | user → agent | The agent drafts `specs/<feature>/plan.md` from both approved specs |
| `sdd plan new <feature>` | user or agent | Create `plan.md` from the template, with the source hashes stamped |
| `sdd plan approve <feature>` | **user** | Freeze the plan (validation and coverage must pass) |

The agent calls `guide` with topic `plan` and the feature. Besides the format and rules, it returns a **coverage checklist** built by `sdd` from the approved specs: every active FR and AC with its text and the components or test level the technical spec assigned to it, the NFRs, the file limit from `sdd.yaml`, the Architecture section (whose layers the groups must follow) and the language. The agent proposes the groups to the user, creates the plan with `plan_new`, writes it, and calls `lint` until validation, coverage and the layer check pass. Then it asks the user to review and approve.

Plan rules:
- **Groups (`G#`)** are ordered by layer, for example `domain → persistence → service → interface → integration`. Each group must be implementable and testable once the groups before it are done. Dependencies (`depends_on`) may only point to earlier groups.
- **Tasks (`T<group>.<n>`)** are small enough to review by hand: at most `limits.max_files_per_task` files each (default 5).
- Every `FR-###` and `AC-###` must be covered by at least one task.

Validation (deterministic):
- IDs are unique and correctly formatted.
- The `depends_on` graph is acyclic and points only backwards.
- Every requirement and criterion is covered.
- Every referenced ID exists in the specs.
- Tasks stay within the size limits.
- Every group's layer is named in the technical spec's Architecture section (a warning otherwise).

## Phase 3: Implement (agent implements, `sdd` verifies, user reviews each group)

| Command | Who | Purpose |
|---|---|---|
| `sdd implement <feature>` | user | Create (or repair) the worktree `.sdd/worktrees/<feature>` on branch `sdd/<feature>` from the current `HEAD`, and start the first group. The repository needs at least one commit |
| `/sdd-implement <feature>` | user → agent | Work through the group's tasks with `task_next` / `task_done`, then `group_verify` |
| `sdd task next <feature>` / `sdd task done <feature> [--summary "…"]` | agent | CLI forms of the MCP tools |
| `sdd group verify <feature>` | agent | CLI form of `group_verify` |
| `sdd review <feature> [--diff]` | **user** | Show the group: its commit, tasks with covered IDs and the agent's summaries, the verification results, the changes (`--diff` for the full diff) and any files changed outside the declared ones |
| `sdd approve <feature>` | **user** | Approve the group under review; the agent's next `task_next` starts the next group |
| `sdd revise <feature> -m "<feedback>"` | **user** | Send the group back with feedback; the agent gets it from `task_next` |
| `sdd resume <feature> [--discard] [--force-unlock]` | **user** | Repair state after an interruption (see [TRACEABILITY.md](TRACEABILITY.md)) |
| `sdd clean <feature> [--force]` | **user** | Remove the worktree; the branch and its commits are kept, and the next command that needs the worktree recreates it |

For each group:
1. `task_next` returns the next pending task, or the one still in progress after an interruption: its description, `files`, `done_when`, `verify`, the text of every FR and AC it covers, the language profile `.sdd/profiles/<language>.md` and the worktree path. The task becomes `in-progress`.
2. The agent implements the task in the worktree, with unit tests that reference the criteria (`covers: AC-###`).
3. `task_done` runs the technical spec's `build` command in the worktree. If it fails, the output goes back to the agent and the task stays in progress. If it passes, `sdd` makes a **WIP checkpoint commit**, marks the task `done` and reports any changed file that the task did not declare.
4. When every task is done, `group_verify` runs `build`, `lint` and `test`. A failure goes back to the agent to fix and counts as one fix attempt; after `limits.max_fix_attempts` the group is marked `failed` and the agent stops and tells the user.
5. On success `sdd` **squashes** the WIP commits into one group commit:
   ```
   feat(<feature>): G2 <group title>

   SDD-Feature: <feature>
   SDD-Group: G2
   SDD-Tasks: T2.1, T2.2, T2.3
   SDD-Covers: FR-001, AC-001, AC-002
   SDD-Run: <run-id>
   ```
6. The group moves to `awaiting-review`. `group_verify` tells the agent to stop and ask the user to review; `task_next` returns "stop" until the user approves, and after the approval it starts the next group.

The agent never commits itself, and a group never advances while the code fails to build or its tests fail. After `sdd revise`, `task_next` returns the feedback (and the group's changes so far) instead of a task; the agent addresses it and calls `group_verify` again, which squashes the group into a single commit once more. A `failed` group also goes back to the agent through `sdd revise`, with guidance. If a spec or the plan is edited during implementation, the feature leaves `implementing` (it becomes stale) and every implementation command stops until the artifacts are re-approved.

The verification commands run with the project's own shell (`sh -c`, or `cmd /C` on Windows) in the worktree and are killed, with their child processes, after `limits.command_timeout`. The last 16 KB of their output are kept for the agent and the review.

## Phase 4: Validate (integration + synchronization)

| Command | Who | Purpose |
|---|---|---|
| `sdd validate <feature>` | user | Once every group is approved: run the `integration` command in the worktree and every sync check, then write `.sdd/features/<feature>/report.md`. If everything passes, the feature becomes `validated` |
| `sdd sync check [<feature>]` | anyone | Check consistency between artifacts. Available in every phase; the plan ↔ code level applies as soon as implementation has started |
| `sdd trace <ID>` / `sdd trace --matrix <feature>` | anyone | Follow one ID through spec → plan → commits → tests, or print the full matrix (with Commits and Tests columns once implementation has started) |
| `/sdd-deep-review <feature>` | user → agent | Semantic review of the code against each AC, written to `.sdd/features/<feature>/deep-review.md` |
| `sdd review <feature> --context` | agent | CLI form of `review_context`: the brief for the deep review |

The report records the verdict, who validated and when, the branch commit, the approved hash of every artifact, the integration output, every consistency problem, the groups with their commits, and the full traceability matrix. It links `deep-review.md` when one exists. After validation, merge `sdd/<feature>` by your usual process and run `sdd clean <feature>`.

Sync levels, each applied as soon as its artifacts exist:
- **spec ↔ spec:** `functional_hash` matches, every FR, NFR and AC is mapped in the technical spec, every ID the technical spec references exists in the functional spec, and every `depends_on` feature exists.
- **spec ↔ plan:** both source hashes match, every FR and AC is covered by a task, and every covered ID exists and is not retired.
- **approvals:** an artifact edited after its approval (`modified`) or whose sources changed after its approval (`stale`) is an error until it is re-approved.
- **plan ↔ code:** every approved group has exactly one commit on `sdd/<feature>` whose `SDD-Group` and `SDD-Tasks` trailers match the plan (a different `SDD-Covers` is a warning); commits on the branch without SDD trailers are reported as made outside `sdd`; every AC of an approved group is referenced by at least one test (`covers: AC-###`), and a reference to an unknown or retired ID is a warning. Test references are searched with `git grep` in every file git sees (outside `specs/` and `.sdd/`), in the worktree or, once it is removed, in the project.

The deep review ("does the code actually satisfy AC-003?") is done by the agent from `review_context`. It only produces a report and never blocks a gate.

## Adopting an Existing Project (brownfield)

The four phases above assume new code. A project that already has code first records **what exists** as baseline specs, without changing the code, and then adds new work as ordinary features on top.

| Command | Who | Purpose |
|---|---|---|
| `/sdd-adopt` | user → agent | Explore the codebase and document it as one or more baseline features |
| `sdd adopt --context` | agent | CLI form of `adopt_context`: languages, build files, detected build/lint/test commands, top-level layout, test directories and existing docs |
| `sdd spec new <feature> --baseline` | user or agent | Create a baseline feature (`origin: existing` in the frontmatter) |
| `sdd spec approve <feature> --functional/--technical` | **user** | Approve each baseline spec as usual |

The adoption flow:
1. The agent reads `adopt_context`, explores the code and proposes how to split it into baseline features (by capability or module; a small project can use a single `baseline` feature). The user confirms the split.
2. For each baseline feature the agent drafts a **functional spec of the current behavior**: observable requirements and acceptance criteria as the system works today, with known problems recorded as such rather than fixed.
3. It drafts a **technical spec of the current design**: the existing packages as components, the real build, lint, test and integration commands, and decisions recorded as ADRs where they can be inferred.
4. The same lint and sync rules apply. When the user approves both specs, the feature becomes `baselined`. There is no plan and no implementation: the code is the implementation.

Afterwards:
- New features declare `depends_on: [<baseline feature>]` and go through the four phases. Their technical specs reuse the baseline's commands and components.
- Baseline traceability stops at the technical spec. Existing tests need no `covers:` references, and the plan ↔ code checks do not apply to baseline features.
- When a later feature changes adopted behavior, the baseline spec is revised in the same way as any spec (retire or amend its IDs and re-approve), so the baseline keeps describing the code as it is.

## Commands at a Glance

```
sdd init [--agent claude|generic] [--check] [--force] | version | status [<feature>] | guide <topic> [--feature <f>] | mcp | upgrade
sdd adopt --context
sdd feature describe <feature> [--title "<title>"] [--description "<text>"]
sdd spec new [--technical|--baseline]|lint|approve
sdd plan new <feature> | plan approve <feature>
sdd implement | review | approve | revise | resume | clean <feature>
sdd task next|done <feature> · sdd group verify <feature>        (used by the agent)
sdd validate <feature>
sdd sync check [<feature>]
sdd trace <ID> | trace --matrix <feature>
```
