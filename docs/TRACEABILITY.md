# Traceability, Audit and Recovery

## 1. The ID Chain

Every change can be traced from requirement to test, and back again:

| Artifact | Carries the ID as |
|---|---|
| `functional.md` | `FR-###`, `NFR-###`, `AC-###` definitions |
| `technical.md` | Requirement Traceability table (FR/NFR → component), Testing Strategy (AC → test level) |
| `plan.md` | `covers: [...]` on each task `T#.#` |
| Git commit | Trailers `SDD-Feature`, `SDD-Group`, `SDD-Tasks`, `SDD-Covers`, `SDD-Run` |
| Tests | A `covers: AC-###` comment or a reference in the test name |

```
$ sdd trace AC-003
AC-003  (user-signup/functional.md · FR-001)
  ├─ technical: SignupService (unit), HTTP handler (integration)
  ├─ plan:      T2.1, T4.2
  ├─ commits:   a1b2c3d G2 "Signup service", 9f8e7d6 G4 "HTTP API"
  └─ tests:     internal/signup/service_test.go:TestSignup_RejectsDuplicateEmail
```

An ID can be qualified with its feature (`sdd trace user-signup/AC-003`); unqualified, every feature is searched. Besides FR, NFR and AC, `sdd trace` also shows ADRs, groups (`G#`) and tasks (`T#.#`, with their group commit). Commits come from the group commits' trailers on `sdd/<feature>` and tests from `covers:` references; before implementation starts they show as "not implemented yet".

`sdd trace --matrix <feature>` prints the full requirement × artifact matrix in markdown, with gaps marked **missing**. `sdd sync check` reports every gap it finds in this chain.

## 2. Audit Journal

`.sdd/features/<feature>/journal.jsonl` holds the audit history:
- It is **append-only**. Each line is one event, and the file is committed with the project.
- `log.md` is a human-readable view generated from it.
- Events are never edited or deleted.

```json
{"seq":42,"ts":"2026-11-17T10:21:03Z","ref":37,"run_id":"20261117-102001-ab12","actor":"agent:claude-opus-5-5",
 "phase":"implement","action":"task.run","status":"completed","ids":["T2.1","AC-003"],
 "hashes":{"plan":"sha256:…"},"usage":{"input_tokens":10234,"output_tokens":1820},"detail":"…"}
```

| Field | Meaning |
|---|---|
| `seq` | Monotonic sequence number per feature |
| `actor` | `user:<git user.email>` for CLI commands, `agent:<client>` for MCP calls (the client name the agent reports, e.g. `agent:claude-code`) |
| `action` | e.g. `feature.describe`, `spec.create`, `spec.approve`, `plan.create`, `plan.approve`, `task.start`, `task.done`, `group.verify`, `group.committed`, `group.approved`, `group.revise` |
| `status` | `started`, `completed`, `failed` (see write-ahead below) or `info` for standalone events |
| `ref` | On `completed` or `failed`: the `seq` of the matching `started` event |
| `hashes` | Hashes of the artifacts involved. Every approval records the exact hash it approved |

### Conversations
The conversation with the agent (prompts, reasoning, tool calls) lives in the host agent's own session history, not in `sdd`. What `sdd` records is what it was asked to do and what it decided: every MCP tool call that changes state is journaled with the agent as actor, and every verification result is journaled with its command, exit status and a digest of its output.

## 3. Checkpoints and Recovery

Recovery rests on three principles:
- **The task is the atomic unit of work.**
- **Every step is persisted before the next one starts.**
- **Git is the source of truth for code.**

### What is persisted

| Mechanism | Where | Purpose |
|---|---|---|
| `state.json` | `.sdd/features/<feature>/` | Feature title and description (the human side of the feature name), who created it and when, the approvals, the current phase, group and task, the status of each task, attempts, `run_id`, worktree, branch and last checkpoint SHA. Written atomically (temp file + `rename`) |
| Write-ahead events | `journal.jsonl` | A `started` event is appended before an action, and a `completed` or `failed` event whose `ref` points to it is appended after. A `started` event that no other event references marks an interrupted action |
| WIP commits | branch `sdd/<feature>` | One checkpoint commit per finished task, squashed into a single commit when the group passes |
| Artifacts on disk | `specs/<feature>/` | The agent writes specs and the plan directly to their files, so a new session continues from what is on disk |
| Lock | `.sdd/locks/<feature>.lock` | PID, host and start time. Prevents two runs on the same feature at once |

### Failure handling
- **The agent session ends** (closed, crashed, out of context): nothing is lost. Artifacts are on disk, state and journal are current, and finished tasks are committed. In a new session, `/sdd-implement` (or `task_next`) returns the task that was in progress.
- **Verification fails:** the output goes back to the agent. A group that still fails after `limits.max_fix_attempts` is marked `failed` and waits for the user.
- **A verification command hangs:** it is killed after `limits.command_timeout` and counts as a failure.
- **Ctrl+C / SIGTERM on an `sdd` command:** the command's context is cancelled, any running verification is stopped, state is saved and the command exits cleanly.
- **Crash or power loss:** the next command sees an interrupted action in the journal and a lock whose process is gone, and offers `sdd resume`.

### `sdd resume <feature>`
1. **Acquire the lock.** A lock whose PID is no longer alive on the same host is taken over. `--force-unlock` overrides any other lock.
2. **Reconcile the journal.** Every `started` event without a matching end is closed with a `failed` event (detail: interrupted).
3. **Check artifact hashes.** If a spec or the plan changed since approval, the feature is no longer `implementing` (see the staleness rule): `resume` reports the next step and the affected artifacts must be re-approved.
4. **Check the worktree.**
   - If it is missing, recreate it from branch `sdd/<feature>` (every sdd command that needs the worktree does this too).
   - If `HEAD` differs from the last checkpoint SHA, warn: commits were made outside `sdd`. State is not rebuilt from the branch; inspect the history, and use `--discard` to return to the checkpoint if needed.
5. **Handle leftover changes** from the interrupted task:
   - `--keep` (default): keep the changes; `task_next` hands the current diff to the agent with the task.
   - `--discard`: reset the worktree to the last checkpoint and restart the task from scratch.
6. **Report the next step.** Phases 1 and 2 need no repair (the artifacts are on disk); in Phase 3 the next `task_next` continues with the first task that is not done.

`sdd status <feature>` shows the current state, the progress of every group and task, any interrupted action, the lock holder and the exact next command to run.

`sdd resume` and `sdd clean` can throw work away, so the permission rules installed by `sdd init --agent claude` deny them to the agent along with the approvals.
