# End-to-end test in Claude Code

A manual walkthrough of the whole workflow (specify → plan → implement → validate) with a real Claude Code session, on a throwaway project. Run it on any machine with the repository checked out.

## 0. Requirements
- Go 1.25, git and Claude Code installed and logged in.
- `sdd` on the `PATH`: `.mcp.json` launches it as `sdd mcp`.

```sh
cd ~/Development/quipu-sdd   # or wherever you cloned it
git pull
make install          # installs ~/go/bin/sdd
which sdd && sdd version
```
If `which` finds nothing, add `~/go/bin` to the `PATH` (e.g. in `~/.zshrc`).

## 1. Create a test project (small Go project)
```sh
mkdir -p ~/tmp/sdd-demo && cd ~/tmp/sdd-demo
git init
go mod init example.com/demo
sdd init --agent claude
git add -A && git commit -m "init"   # sdd implement needs at least one commit
```
`init` creates:
- `sdd.yaml`;
- `.sdd/`, with the templates, the Go profile and its `.gitignore`;
- the six skills in `.claude/skills/`;
- `.mcp.json`;
- `.claude/settings.json`, which allows `mcp__sdd__*` and denies the approval commands.

## 2. Open Claude Code
```sh
claude
```
- Approve the `sdd` MCP server when asked.
- Check with `/mcp` that `sdd` is connected with its 12 tools.
- Type `/` and check that `/sdd-spec`, `/sdd-tech`, `/sdd-plan`, `/sdd-implement`, `/sdd-deep-review` and `/sdd-adopt` are listed.

## 3. Specify (functional)
In Claude Code:
```
/sdd-spec A to-do list CLI: add, list and complete tasks stored in a JSON file
```
Claude should:
- create `specs/<feature>/functional.md` with the `spec_new` tool (sdd derives the feature name from the text);
- fill it in;
- check it with `lint`.

In **another terminal**, review and approve:
```sh
cd ~/tmp/sdd-demo
sdd status
sdd spec lint <feature>
sdd spec approve <feature> --functional
```

## 4. Specify (technical)
```
/sdd-tech <feature>
```
Check that the technical spec defines the verification commands (e.g. `go build ./...`, `go test ./...`). Then:
```sh
sdd spec approve <feature> --technical
```

## 5. Plan
```
/sdd-plan <feature>
```
```sh
sdd sync check <feature>
sdd plan approve <feature>
```

## 6. Implement (group by group)
```sh
sdd implement <feature>        # creates .sdd/worktrees/<feature> on branch sdd/<feature>
```
In Claude Code:
```
/sdd-implement <feature>
```
For each task, Claude calls `task_next`, writes the code, then calls `task_done`. At the end of the group it calls `group_verify`, and it stops when the group is waiting for your review. Then, in the terminal:
```sh
sdd review <feature>            # summary; --diff for the full diff
sdd approve <feature>           # or: sdd revise <feature> -m "what to change"
```
Repeat `/sdd-implement <feature>` for each group until the plan is done.

## 7. Validate
```sh
sdd validate <feature>          # writes .sdd/features/<feature>/report.md
sdd trace --matrix <feature>
git log --format='%h %s%n%(trailers)' sdd/<feature>
```
Optional: `/sdd-deep-review <feature>` in Claude Code.

## 8. Safety checks
- **Ask Claude** to run `sdd spec approve <feature> --functional`. The `deny` rule in `.claude/settings.json` must block it. Through MCP, the core also rejects it (`ErrHumanOnly`).
- **Type `! sdd plan approve <feature>`** yourself inside Claude Code. This is unverified: whether `deny` rules apply to `!` bash mode. Record what happens.
- **Interruption:** quit Claude Code in the middle of a task, run `sdd resume <feature>`, then continue with `/sdd-implement`.

## Brownfield variant
On an existing project, replace steps 3–5 with `/sdd-adopt`, then approve the baseline spec with `sdd spec approve <feature> --functional`. Baseline features have no plan.

## What to record
- Whether Claude used the MCP tools or fell back to the shell.
- Whether the skills triggered correctly.
- Where Claude got confused, and which sdd messages were ambiguous.
- Approximate token usage (`/cost` or `/context`).

These notes drive the fixes before M8.
