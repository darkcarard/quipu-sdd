# sdd: Spec-Driven Development Framework

`sdd` is a single-binary, model-agnostic CLI that drives software development from approved specifications. You work in four gated phases:

1. **Specify:** write the functional spec, then the technical spec, together with the agent. You approve each one.
2. **Plan:** the agent turns the specs into layered, reviewable task groups. You approve the plan.
3. **Implement:** the agent implements one group at a time in an isolated git worktree. It builds and tests, makes one commit per group, and waits for your review.
4. **Validate:** integration tests run, and the specs, plan and code are checked for synchronization.

Every action is journaled and checkpointed. Requirement IDs can be traced from the spec to commits and tests, and an interrupted run can be resumed.

> Status: **M0 (scaffold)**. Only `sdd init` and `sdd version` work so far. See [docs/ROADMAP.md](docs/ROADMAP.md).

## Install

```sh
go install github.com/darkcarard/sdd-framework/cmd/sdd@latest
# or, from a clone:
make install
```

## Use in a project

```sh
cd your-project        # a git repository
sdd init               # creates sdd.yaml, specs/, .sdd/ and updates .gitignore
export ANTHROPIC_API_KEY=...   # or put it in .env (git-ignored)
```

`sdd init` creates the following, and you commit all of it with your project except the git-ignored ephemeral paths:

```
sdd.yaml                     # provider, limits, MCP servers
specs/<feature>/             # functional.md, technical.md, plan.md
.sdd/templates/, profiles/   # customizable templates and language profiles
.sdd/features/<feature>/     # state.json, journal.jsonl, log.md, report.md
```

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
