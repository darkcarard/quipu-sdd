# Drafting a technical spec

A technical spec says **how** the approved functional spec will be built. It can only be drafted once the functional spec is approved, and every requirement and criterion of that spec must be mapped in it.

## How to draft it

1. Create the file with `spec_new` (with `technical: true`) if it does not exist, then read `specs/<feature>/technical.md` and the approved `specs/<feature>/functional.md`.
2. Study the existing code base before proposing a design: its language, layout, conventions, and its real build, lint and test commands.
3. Propose the architecture and components to the user and agree on the significant decisions before writing them down.
4. Fill in the frontmatter: `language` selects the profile `.sdd/profiles/<language>.md` (read it if it exists), and `commands` must be the project's real commands: `build`, `lint` and `test` are required; `integration` runs the integration tests (leave it empty only if there are none, and tell the user).
5. After every edit, call `lint` and `sync_check` for the feature and fix every error.
6. When both pass and the user is satisfied, ask them to run `sdd spec approve <feature> --technical`. Never run it yourself. (Approval stamps `functional_hash` automatically; do not edit it.)

## Format rules (checked by lint and sync)

- Required sections, in this order: Overview, Architecture, Components, Data Model, Interfaces / APIs, Requirement Traceability, Error Handling, Testing Strategy, Decisions, Risks.
- **Requirement Traceability** is a table whose first column lists one or more FR/NFR IDs and whose second column lists the components that implement them (comma-separated). Every active FR and NFR of the functional spec must appear.
- **Testing Strategy** is a table whose first column lists AC IDs and whose second column gives the test level: `unit`, `integration` or `e2e` (several may be listed). Every active AC must appear.
- Reference only IDs that exist in the functional spec. Retired IDs should not be mapped.
- Every significant decision is an ADR: `### ADR-001: <title>` with Context, Decision and Consequences bullets. ADR IDs follow the same rules as other IDs: three digits, sequential, never renumbered.
- Name the layers in Architecture (for example `domain → persistence → service → interface`): the plan will group tasks by them.
- Replace every template placeholder.
