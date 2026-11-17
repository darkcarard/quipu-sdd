# Agent instructions

{{workflow}}

## Tools

If your agent supports MCP, register the `sdd` server: it runs over stdio with the command `sdd mcp` from the project root, and exposes the tools named above. Otherwise use the CLI:

| Purpose | Command |
|---|---|
| Phase and next step | `sdd status [<feature>]` |
| Drafting instructions and template | `sdd guide functional\|technical\|plan [--feature <feature>]` |
| Create a feature / its technical spec | `sdd spec new "<feature or description>"` / `sdd spec new <feature> --technical` |
| Create the plan | `sdd plan new <feature>` |
| Lint | `sdd spec lint [<feature>]` |
| Consistency between artifacts | `sdd sync check [<feature>]` |
| Follow an ID | `sdd trace <ID>` |

The user runs the approvals: `sdd spec approve <feature> --functional|--technical` and `sdd plan approve <feature>`.
