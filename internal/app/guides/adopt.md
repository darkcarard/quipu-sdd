# Adopting an existing project

An existing project is documented as one or more **baseline features**: specs that describe the code as it is today, so new work can build on them. Adoption never changes the code.

## How to do it

1. Call `adopt_context`: an inventory of the project (languages, build files, suggested commands, layout, tests, docs) and the features sdd already knows.
2. Explore the code and propose how to split it into baseline features, by capability or module (a small project can be a single `baseline`). Agree on the split with the user before writing anything.
3. For each baseline feature:
   - Create it with `spec_new` and `baseline: true`, then write the **functional spec of the current behavior**: what the system does today, as FRs with Given/When/Then criteria you can confirm in the code or its tests. Do not write requirements for behavior the code does not have. Record known defects as such (e.g. "Known issue: …" under Constraints), not as requirements.
   - Run `lint` after every edit. When it passes and the user agrees, ask them to approve with `sdd spec approve <feature> --functional`.
   - Create the technical spec with `spec_new` and `technical: true`: the existing packages as components, the real build, lint, test and integration commands (confirm them with the user, starting from the suggested ones), decisions you can infer recorded as ADRs. Run `lint` and `sync_check`; the user approves with `sdd spec approve <feature> --technical`.
4. Once both specs are approved the feature is `baselined`. A baseline has no plan and no implementation.

Afterwards, new work is a normal feature that declares `depends_on: [<baseline>]` in its functional spec. If it changes behavior the baseline describes, the baseline's specs are revised (retire or amend IDs) and re-approved, so they keep describing the code as it is. Existing tests may reference baseline criteria with `covers: AC-###`; it is optional.

All the format rules of the functional and technical guides apply (`guide functional`, `guide technical`).
