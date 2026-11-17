# Agent instructions

{{workflow}}

## Tools

If your agent supports MCP, register the `sdd` server: it runs over stdio with the command `sdd mcp` from the project root, and exposes the tools named above. Otherwise use the CLI:

| Purpose | Command |
|---|---|
| Phase and next step | `sdd status [<feature>]` |
| Drafting instructions and template | `sdd guide functional\|technical\|plan\|implement [--feature <feature>]` |
| Create a feature / its technical spec | `sdd spec new "<feature or description>"` / `sdd spec new <feature> --technical` |
| Create the plan | `sdd plan new <feature>` |
| Lint | `sdd spec lint [<feature>]` |
| Consistency between artifacts | `sdd sync check [<feature>]` |
| Follow an ID | `sdd trace <ID>` |
| Adopt existing code as baseline specs | `sdd adopt --context`, `sdd guide adopt`, `sdd spec new <feature> --baseline` |
| Implementation: next step / finish a task / verify the group | `sdd task next <feature>` / `sdd task done <feature> --summary "…"` / `sdd group verify <feature>` |

The user runs the approvals and reviews: `sdd spec approve <feature> --functional|--technical`, `sdd plan approve <feature>`, `sdd implement <feature>`, `sdd review`, `sdd approve`, `sdd revise`, `sdd resume`, `sdd clean` and `sdd merge`.
