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

## State Machine

The feature state is stored in `.sdd/features/<feature>/state.json`:

| State | Meaning | Leaves via |
|---|---|---|
| `functional-drafting` | Functional spec being written with the agent | `sdd spec approve --functional` |
| `functional-approved` | Functional spec frozen at a hash | `sdd spec new --technical` (or `sdd spec chat --technical`) |
| `technical-drafting` | Technical spec being written | `sdd spec approve --technical` |
| `technical-approved` | Both specs frozen | `sdd plan` (or `sdd plan new`) |
| `plan-drafting` | Plan generated, awaiting review | `sdd plan approve` |
| `plan-approved` | Plan frozen | `sdd implement` |
| `implementing` | Groups being executed (see group states) | last group approved |
| `implemented` | All groups committed and approved | `sdd validate` passes |
| `validated` | Integration tests and sync checks pass | — |

Up to `plan-approved`, the phase is **derived from the artifacts**: their existence, the approvals in `state.json` and the current content hashes (see the staleness rule below). Editing a file by hand or pulling a teammate's changes is therefore always reflected by `sdd status`, and an edited or stale artifact reopens its `*-drafting` phase. From `implementing` onwards, the phase stored in `state.json` is kept as long as every artifact is still approved. The stored phase is updated on the next command that writes state, such as an approval.

**Group states:** `pending → in-progress → awaiting-review → approved`. A `sdd revise` request sends a group from `awaiting-review` back to `in-progress`.

**Task states:** `pending → in-progress → done`, plus `failed` and `interrupted`.

### Staleness Rule
Each approval stores the SHA-256 of the approved artifact. Each downstream artifact stores the hashes of its sources:
- `technical.md` stores `functional_hash`.
- `plan.md` stores `functional_hash` and `technical_hash`.

These source hashes are **stamped automatically** into the frontmatter when the downstream artifact is created and again when it is approved, and the approval in `state.json` records them as well. Hashes normalize CRLF to LF, so a checkout with `core.autocrlf` does not look like an edit.

If a source changes after approval, every artifact that depends on it becomes **stale**. The feature cannot move forward until the stale artifacts are reconciled and approved again. For example, editing an approved functional spec makes both the technical spec and the plan stale.

## Phase 1: Specify (user + agent, iterative)

| Command | Purpose |
|---|---|
| `sdd spec new <feature> [--title "<title>"]` | Create `specs/<feature>/functional.md` from the template |
| `sdd spec new <feature> --technical` | Create `technical.md` from the template to write it by hand. Only allowed after functional approval |
| `sdd spec chat <feature> --functional` | Interactive drafting session with the agent |
| `sdd spec lint [<feature>]` | Deterministic guardrail checks (see [SPEC_GUIDELINES.md](SPEC_GUIDELINES.md)) |
| `sdd spec approve <feature> --functional` | Freeze the functional spec (lint must pass) |
| `sdd spec chat <feature> --technical` | Draft `technical.md`. Only allowed after functional approval |
| `sdd spec approve <feature> --technical` | Freeze the technical spec (lint + sync must pass) |

The drafting loop:
1. The agent drafts or updates sections and asks clarifying questions.
2. The user answers or edits the file directly.
3. Lint runs after every agent turn and reports violations.
4. The loop repeats until the user approves.

The agent may only edit the spec file being drafted, and it writes the file to disk after every turn. The session history is saved in `.sdd/features/<feature>/sessions/` so a chat can be resumed.

Ordering is strict: functional first, then technical. The technical spec must map every `FR-###` and `NFR-###` to at least one component, and every `AC-###` to a test type.

## Phase 2: Plan (agent)

| Command | Purpose |
|---|---|
| `sdd plan <feature>` | Agent generates `specs/<feature>/plan.md` from both approved specs |
| `sdd plan new <feature>` | Create `plan.md` from the template to write it by hand |
| `sdd plan approve <feature>` | Freeze the plan (validation and coverage must pass) |

Plan rules:
- **Groups (`G#`)** are ordered by layer, for example `domain → persistence → service → interface → integration`. Each group must be implementable and testable once the groups before it are done. Dependencies (`depends_on`) may only point to earlier groups.
- **Tasks (`T<group>.<n>`)** are small enough to review by hand: at most `limits.max_files_per_task` files each (default 5).
- Every `FR-###` and `AC-###` must be covered by at least one task.

The generated plan is written to a temporary file, validated, and only then moved into place. If generation is interrupted, it can simply be run again.

Validation (deterministic):
- IDs are unique and correctly formatted.
- The `depends_on` graph is acyclic and points only backwards.
- Every requirement and criterion is covered.
- Every referenced ID exists in the specs.
- Tasks stay within the size limits.

## Phase 3: Implement (agent, gated per group)

| Command | Purpose |
|---|---|
| `sdd implement <feature>` | Run the next pending group in worktree `.sdd/worktrees/<feature>` (branch `sdd/<feature>`) |
| `sdd review <feature>` | Show the group summary, the diff, test results and covered IDs |
| `sdd approve <feature>` | Approve the group under review and continue with the next one |
| `sdd revise <feature> -m "<feedback>"` | Send review feedback; the agent fixes and re-verifies the same group |
| `sdd resume <feature>` | Continue after an interruption (see [TRACEABILITY.md](TRACEABILITY.md)) |

For each group:
1. For each task, run an agent loop with MCP tools (confined to the worktree). Inject the language profile `.sdd/profiles/<language>.md` and the task's spec excerpts into the prompt.
2. When the task builds, make a **WIP checkpoint commit**.
3. When all tasks are done, run the technical spec's `build`, `lint` and `test` commands.
4. If verification fails, run a fix loop for up to `limits.max_fix_attempts`. If it still fails, the group is marked `failed` and the run stops for the user to inspect.
5. On success, **squash** the WIP commits into one group commit:
   ```
   feat(<feature>): G2 <group title>

   SDD-Feature: <feature>
   SDD-Group: G2
   SDD-Tasks: T2.1, T2.2, T2.3
   SDD-Covers: FR-001, AC-001, AC-002
   SDD-Run: <run-id>
   ```
6. The group moves to `awaiting-review` and the run stops.

The agent never moves to the next group while the code fails to build or unit tests fail.

## Phase 4: Validate (integration + synchronization)

| Command | Purpose |
|---|---|
| `sdd validate <feature>` | Run the `integration` command and every sync check, then write `.sdd/features/<feature>/report.md` |
| `sdd sync check [<feature>] [--deep]` | Check consistency between artifacts. Available in every phase |
| `sdd trace <ID>` / `sdd trace --matrix <feature>` | Follow one ID through spec → plan → commits → tests, or print the full matrix |

Sync levels, each applied as soon as its artifacts exist:
- **spec ↔ spec:** `functional_hash` matches, every FR, NFR and AC is mapped in the technical spec, every ID the technical spec references exists in the functional spec, and every `depends_on` feature exists.
- **spec ↔ plan:** both source hashes match, every FR and AC is covered by a task, and every covered ID exists and is not retired.
- **approvals:** an artifact edited after its approval (`modified`) or whose sources changed after its approval (`stale`) is an error until it is re-approved.
- **plan ↔ code:** every approved group has exactly one commit with matching trailers, and every AC is referenced by at least one test (`covers: AC-###`).
- **`--deep`:** an LLM-assisted semantic review (for example, "does the code actually satisfy AC-003?"). It only produces a report and never blocks a gate on its own.

## Commands at a Glance

```
sdd init | version | status [<feature>] | upgrade
sdd spec new|chat|lint|approve
sdd plan <feature> | plan new <feature> | plan approve <feature>
sdd implement | review | approve | revise | resume | clean <feature>
sdd validate <feature>
sdd sync check [<feature>] [--deep]
sdd trace <ID> | trace --matrix <feature>
```
