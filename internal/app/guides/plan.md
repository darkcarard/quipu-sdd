# Drafting the plan

The plan turns the two approved specs into ordered groups of small tasks. Each group is implemented, verified and committed as one unit and reviewed by the user before the next one starts, so the plan decides how the work will be reviewed.

## How to draft it

1. Both specs must be approved; `status` tells you. Create the file with `plan_new` if it does not exist (it stamps the hashes of the specs it is based on; do not edit them), then read `specs/<feature>/plan.md`, both specs and the coverage checklist below.
2. Look at the code base to decide which files each task touches.
3. Propose the groups (titles, layers, order) to the user before writing every task.
4. Write the plan. After every edit, call `lint` for the feature and fix every error: it checks numbering, dependencies, task fields, file limits, coverage and the layers.
5. When lint passes and the user agrees, ask them to approve it with `sdd plan approve <feature>`. Never run it yourself.

## Format (checked by lint)

```markdown
## G1: <group title>
- layer: domain
- depends_on: []
- goal: <the increment this group delivers, in one sentence>

### T1.1: <task title>
- covers: [FR-001, AC-001]
- files: [internal/order/order.go, internal/order/order_test.go]
- done_when: <observable completion condition>
- verify: <the unit test name or pattern that proves it>

<Short description of the change.>
```

- Groups are numbered `G1, G2, …` in order; tasks `T<group>.<n>`, from 1 within each group.
- Order groups by layer, following the layers named in the technical spec's Architecture (for example domain → persistence → service → interface → integration). Each group must build and pass its tests once the groups before it are done; `depends_on` may only name earlier groups.
- Aim for roughly 3 to 8 groups. A feature that needs more should be split.
- Every task lists `covers`, `files`, `done_when` and `verify`. Keep tasks small enough to review by hand: at most the number of files given below.
- A task's unit tests live in the same task or group as the code they test, and reference the criteria they verify (`covers: AC-###`).
- Every active FR and AC must be covered by at least one task; cover NFRs where a task addresses them. Cover only IDs that exist and are not retired.
- Replace every template placeholder.
