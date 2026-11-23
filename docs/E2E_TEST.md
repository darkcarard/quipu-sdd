# End-to-end test

A manual walkthrough of the whole workflow (specify → plan → implement → validate) with a real agent session, on a throwaway project. The steps are shared; what differs per agent is how it is set up (steps 1–2), how skills are invoked and the safety checks (step 8).

| Agent | `--agent` | Skills | Status |
|---|---|---|---|
| Claude Code | `claude` | `/sdd-spec` | Passed (Claude Code 2.1.326) |
| Codex CLI | `codex` | `$sdd-spec` | Not run yet |
| Antigravity CLI | `antigravity` | `/sdd-spec` | Not run yet |

The steps below write skills as `/sdd-spec`; in Codex type `$sdd-spec`. Run `sdd` commands in a separate terminal (in Claude Code, `! sdd …` also works).

## 0. Requirements
- Go 1.25, git, and the agent installed and logged in.
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
sdd init --agent claude              # or codex, or antigravity
git add -A && git commit -m "init"   # sdd implement needs at least one commit
```
`init` creates `sdd.yaml`, `.sdd/` (templates, the Go profile and its `.gitignore`) and the agent's files (see [WORKFLOW.md](WORKFLOW.md#agent-setup)):
- **Claude Code:** the seven skills in `.claude/skills/`, the `sdd-reviewer` subagent in `.claude/agents/`, `.mcp.json`, and `.claude/settings.json` (allows `mcp__sdd__*`, denies the human-only commands, installs the guard hook).
- **Codex:** the seven skills in `.agents/skills/`, `AGENTS.md`, `.codex/config.toml`, `.codex/hooks.json` and `.codex/rules/sdd.rules`.
- **Antigravity:** the seven skills in `.agents/skills/`, `AGENTS.md`, `.agents/mcp_config.json` and `.agents/hooks.json`.

## 2. Open the agent
**Claude Code:** run `claude` and approve the `sdd` MCP server when asked. Check with `/mcp` that `sdd` is connected with its 12 tools, and type `/` to see `/sdd-spec`, `/sdd-tech`, `/sdd-plan`, `/sdd-implement`, `/sdd-review`, `/sdd-deep-review` and `/sdd-adopt`, and `/agents` to see `sdd-reviewer`.

**Codex:** run `codex` and trust the project when asked (Codex ignores `.codex/` otherwise). Then:
- `/mcp` lists the `sdd` server and its 12 tools;
- `/hooks` shows the `sdd hook pre-tool-use` hook: review and trust it, or it will not run;
- `/skills` (or typing `$`) lists the seven `sdd-*` skills.
Record whether `AGENTS.md` was picked up and whether the MCP tools were used without approval prompts.

**Antigravity:** run `agy` and trust the folder when asked. Then `/mcp` shows the `sdd` server, `/hooks` the `sdd-guard` hook, and typing `/` lists the seven `sdd-*` skills (run `/skills reload` if not).

## 3. Specify (functional)
In the agent:
```
/sdd-spec A to-do list CLI: add, list and complete tasks stored in a JSON file
```
The agent should:
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
In the agent:
```
/sdd-implement <feature>
```
For each task, the agent calls `task_next`, writes the code, then calls `task_done`. At the end of the group it calls `group_verify`, and it stops when the group is waiting for your review. Optionally, ask for an expert review first:
```
/sdd-review G1                  # writes .sdd/features/<feature>/reviews/G1.md
```
In Claude Code, check that it runs in the `sdd-reviewer` subagent (the session receives only its summary) and that it writes nothing but the report. Then, in the terminal:
```sh
sdd review G1                   # opens each changed file side by side in your editor (or prints the diff)
sdd approve G1                  # or: sdd revise G1 -m "what to change"
```
Check that `sdd review G1` opens the diffs without blocking the terminal (also with `!` in Claude Code), and that `SDD_REVIEW_EDITOR=none sdd review G1` prints the diff instead.
Inside Claude Code, prefix them with `!` (`! sdd approve G1`); `/sdd-implement` is a slash command and takes no `!`.
Repeat `/sdd-implement <feature>` for each group until the plan is done.

## 7. Validate
```sh
sdd validate <feature>          # writes .sdd/features/<feature>/report.md
sdd trace --matrix <feature>
git log --format='%h %s%n%(trailers)' sdd/<feature>
sdd merge                       # from any directory: merges into the project's branch, removes the worktree
git log --oneline               # approvals, group commits and the merge records are all on your branch
git status                      # specs/ and .sdd/features/ are clean
```
Optional: `/sdd-deep-review <feature>` in the agent.

## 8. Safety checks
The checks are the same for every agent; the results recorded so far are from Claude Code.
- **Ask the agent** to run, one by one, harmless forms of a human-only command (with nothing under review they only print an sdd error): `sdd approve G9`, `sdd merge`, `cd . && sdd approve G9`, `X=1 sdd approve G9`, `~/go/bin/sdd approve G9`, `bash -c "sdd approve G9"`. Every one must be denied. The agent may refuse on its own first (its instructions forbid approvals); tell it this is a permission test. Through MCP, the core also rejects approvals (`ErrHumanOnly`).
  - First run (Claude Code 2.1.326, deny rules only): the first five were denied by the rules; `bash -c "sdd approve G9"` ran. The `PreToolUse` hook (`sdd hook pre-tool-use`) was added for that case. Second run, with the hook: all six were denied by the hook (it runs before the deny rules, which stay as a fallback).
  - Codex: the commands should be denied by the hook and, without wrappers, by `.codex/rules/sdd.rules` (check also `codex execpolicy check --rules .codex/rules/sdd.rules -- sdd approve G9`). Antigravity: by the hook only.
- **Claude Code only: type `! sdd approve G9`** yourself: it must run (an sdd error, not a permission denial), since hooks do not apply to `!`. Verified: the hook did not intervene and sdd answered `no feature being implemented has G9 as its current group`.
  - Verified in the first run: `!` commands share the agent's shell environment (`CLAUDECODE=1`, the same `AI_AGENT`) and have no TTY, so `sdd` cannot tell them apart from the agent's by itself.
- **Interruption:** quit the agent in the middle of a task, run `sdd resume <feature>`, then continue with `/sdd-implement`.

## Brownfield variant
On an existing project, replace steps 3–5 with `/sdd-adopt`, then approve the baseline spec with `sdd spec approve <feature> --functional`. Baseline features have no plan.

## What to record
- The agent and its version.
- Whether the agent used the MCP tools or fell back to the shell.
- Whether the skills triggered correctly, and how they received their argument (Codex and Antigravity have no `$ARGUMENTS`: the skill reads it from your request).
- Whether the guard hook ran (step 8), and anything the agent's trust prompts asked for.
- Where the agent got confused, and which sdd messages were ambiguous.
- Approximate token usage.
