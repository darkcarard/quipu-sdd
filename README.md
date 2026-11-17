# sdd: Spec-Driven Development Framework

`sdd` is a single-binary CLI that drives software development from approved specifications, together with the coding agent you already use (Claude Code first; Codex CLI, Gemini CLI and others later). `sdd` never calls an LLM itself: the agent does the reasoning under your existing subscription, and `sdd` owns the specs, the guardrails, the approval gates, verification, commits and the audit trail. The agent reaches it through a local MCP server and installed `/sdd-*` commands.

You work in four gated phases:

1. **Specify:** the agent drafts the functional spec, then the technical spec, with you. You approve each one.
2. **Plan:** the agent turns the specs into layered, reviewable task groups. You approve the plan.
3. **Implement:** the agent implements one task at a time in an isolated git worktree. `sdd` builds and tests, makes one commit per group, and waits for your review.
4. **Validate:** integration tests run, and the specs, plan and code are checked for synchronization.

Every action is journaled and checkpointed. Requirement IDs can be traced from the spec to commits and tests, and an interrupted run can be resumed.

> Status: **M5 done.** Specs, the plan and the implementation are done with Claude Code through the `sdd` MCP server and the `/sdd-spec`, `/sdd-tech`, `/sdd-plan` and `/sdd-implement` commands. `sdd` lints, approves, verifies, commits one squashed commit per group on branch `sdd/<feature>`, and stops for your review after each group. Validation arrives in M6. See [docs/ROADMAP.md](docs/ROADMAP.md).

## Install

```sh
go install github.com/darkcarard/sdd-framework/cmd/sdd@latest
# or, from a clone:
make install
```

## Use in a project

```sh
cd your-project        # a git repository
sdd init --agent claude   # sdd.yaml, specs/, .sdd/ and the Claude Code setup
                          # (.mcp.json, /sdd-* skills, permission rules)
```

`sdd init` never modifies an existing file: it creates what is missing and prints what to add by hand for the rest (`sdd init --check` shows what is missing). Then, in Claude Code:

```text
/sdd-spec "User signup with email"     # the agent interviews you and drafts the functional spec
! sdd spec approve user-signup --functional   # you approve (agents cannot)
/sdd-tech user-signup                  # the agent drafts the technical spec
/sdd-plan user-signup                  # then the plan (approve each one as above)
! sdd implement user-signup            # creates the worktree .sdd/worktrees/user-signup
/sdd-implement user-signup             # the agent implements the first group, task by task
! sdd review user-signup               # you review the group's commit
! sdd approve user-signup              # or: sdd revise user-signup -m "..."
```

`sdd init` creates the following, and you commit all of it with your project except the git-ignored ephemeral paths:

```
sdd.yaml                     # limits
specs/<feature>/             # functional.md, technical.md, plan.md
.sdd/templates/, profiles/   # customizable templates and language profiles
.sdd/features/<feature>/     # state.json, journal.jsonl, log.md, report.md
```

## Workflow so far (M1)

```sh
sdd spec new "User Signup" --description "Sign up with email"   # → specs/user-signup/functional.md
sdd spec lint user-signup                # deterministic guardrails
sdd spec approve user-signup --functional
sdd spec new user-signup --technical     # write technical.md, then approve it with --technical
sdd plan new user-signup                 # write plan.md, then: sdd plan approve user-signup
sdd status [user-signup]                 # phase, artifact status and the next command
sdd sync check                           # spec↔spec and spec↔plan consistency, stale approvals
sdd trace AC-002                         # or: sdd trace --matrix user-signup
```

Editing an approved spec sends it back to drafting and marks every artifact derived from it as stale until it is re-approved.

## Documentation

- [docs/BRIEF.md](docs/BRIEF.md): goals, constraints and architecture
- [docs/WORKFLOW.md](docs/WORKFLOW.md): the four phases, state machine and commands
- [docs/SPEC_GUIDELINES.md](docs/SPEC_GUIDELINES.md): spec guardrails and ID conventions
- [docs/TRACEABILITY.md](docs/TRACEABILITY.md): audit journal, traceability and recovery
- [docs/ROADMAP.md](docs/ROADMAP.md): milestones

## Development

```sh
make check   # gofmt check, go vet, go test -race
make build   # bin/sdd
```
