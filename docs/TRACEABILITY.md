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

`sdd trace --matrix <feature>` prints the full requirement × artifact matrix in markdown. `sdd sync check` reports every gap it finds in this chain.

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
| `actor` | `user:<git user.email>` or `agent:<model>` |
| `action` | e.g. `spec.approved`, `plan.generate`, `task.run`, `group.verify`, `group.committed`, `group.approved` |
| `status` | `started`, `completed`, `failed` (see write-ahead below) or `info` for standalone events |
| `ref` | On `completed` or `failed`: the `seq` of the matching `started` event |
| `hashes` | Hashes of the artifacts involved. Every approval records the exact hash it approved |

### Transcripts
`.sdd/runs/<run-id>/transcript.jsonl` records every provider request and response and every tool call with its result. Credentials and anything matching a configured secret pattern are redacted. The directory is git-ignored by default; set `audit.commit_transcripts: true` to version it.

## 3. Checkpoints and Recovery

Recovery rests on three principles:
- **The task is the atomic unit of work.**
- **Every step is persisted before the next one starts.**
- **Git is the source of truth for code.**

### What is persisted

| Mechanism | Where | Purpose |
|---|---|---|
| `state.json` | `.sdd/features/<feature>/` | Current phase, group and task, the status of each task, attempts, `run_id`, worktree, branch and last checkpoint SHA. Written atomically (temp file + `rename`) |
| Write-ahead events | `journal.jsonl` | A `started` event is appended before an action, and a `completed` or `failed` event whose `ref` points to it is appended after. A `started` event that no other event references marks an interrupted action |
| WIP commits | branch `sdd/<feature>` | One checkpoint commit per finished task, squashed into a single commit when the group passes |
| Chat sessions | `.sdd/features/<feature>/sessions/` | Phase 1 conversation history. The draft spec is also saved to disk after every agent turn |
| Lock | `.sdd/locks/<feature>.lock` | PID, host and start time. Prevents two runs on the same feature at once |

### Failure handling
- **Network or provider errors:** exponential backoff with jitter on connection errors and on HTTP 429, 5xx and 529. When retries run out, the run checkpoints and exits with `run "sdd resume <feature>"`.
- **Ctrl+C / SIGTERM:** the run context is cancelled, the current task is marked `interrupted`, state is saved and the process exits cleanly.
- **Crash or power loss:** the next command sees an interrupted action in the journal and a lock whose process is gone, and offers `sdd resume`.

### `sdd resume <feature>`
1. **Acquire the lock.** A lock whose PID is no longer alive on the same host is taken over. `--force-unlock` overrides any other lock.
2. **Reconcile the journal.** Every `started` event without a matching end is marked `interrupted`.
3. **Check artifact hashes.** If a spec or the plan changed since approval, mark the affected artifacts stale and stop for re-approval.
4. **Check the worktree.**
   - If it is missing, recreate it from branch `sdd/<feature>`.
   - If `HEAD` differs from the checkpoint SHA, rebuild group and task state from the `SDD-*` trailers in the branch history.
5. **Handle leftover changes** from the interrupted task:
   - `--discard` (default): reset to the last checkpoint and restart the task from scratch.
   - `--keep`: keep the changes and hand the current diff to the agent as context.
6. **Continue from the recorded phase.**
   - Phase 1: reload the session history and the draft.
   - Phase 2: regenerate the plan. This is safe because the plan is only written after it passes validation.
   - Phase 3: continue with the first task that is not done.

`sdd status <feature>` shows the current state, any interrupted action, the lock holder and the exact next command to run.
