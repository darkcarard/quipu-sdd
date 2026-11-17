# Roadmap

Each milestone ends with `go build ./... && go vet ./... && go test ./...` passing and its documentation updated.

| Milestone | Scope | Exit criteria |
|---|---|---|
| **M0: Scaffold** | Go module, CLI dispatcher, embedded templates and profiles, `sdd init`, `sdd version`, core interfaces (Provider, ToolHub, Workspace), state, journal and lock packages | `sdd init` creates the project layout in an empty repo; unit tests pass |
| **M1: Specs, plans and state** | Spec and plan parsers, deterministic lint, workflow state machine wired to commands, `sdd status`, `sdd spec new/lint/approve`, `sdd plan approve`, staleness detection, `sync check` (spec↔spec, spec↔plan), `trace` over specs and plan | A spec written by hand can be linted, approved and traced; editing it marks downstream artifacts stale |
| **M2: Claude provider** | Anthropic Messages API over `net/http`, SSE streaming, tool use, retries and backoff, usage accounting, redacted transcripts | Contract tests against SSE and HTTP fixtures in the documented wire format; an optional live smoke test (`make live-test`, build tag `live`) runs when `ANTHROPIC_API_KEY` is set |
| **M3: Phase 1 (Specify)** | `sdd spec chat` loop with lint feedback, saved sessions, resumable chat | A functional and a technical spec are drafted and approved end to end with the agent |
| **M4: Phase 2 (Plan)** | `sdd plan` generation, then validation, then atomic write | A generated plan passes validation and covers every FR and AC |
| **M5: Phase 3 (Implement)** | MCP hub (official Go SDK) with allowlist and path confinement, git worktrees, agent loop per task, verification and fix loop, WIP checkpoints squashed into group commits with trailers, `review`, `approve`, `revise`, `resume`, `clean` | A sample feature is implemented group by group; killing the process mid-task and running `sdd resume` continues correctly |
| **M6: Phase 4 (Validate)** | `sdd validate`, plan↔code sync, full `trace` including commits and tests, `--deep` semantic review, `report.md` | `sdd validate` produces a passing report for the sample feature |
| **M7: More providers** | Google Gemini and OpenAI Responses API adapters | The same sample feature runs end to end on each provider |
| **Later** | Docker MCP tools, more language profiles, `sdd upgrade` migrations, parallel features | — |
