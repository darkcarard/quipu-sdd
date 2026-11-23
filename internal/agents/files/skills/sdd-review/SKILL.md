---
name: sdd-review
description: Expert review of an sdd (Spec-Driven Development) implementation group (its code, the criteria it covers, the language profile and the project's guardrails) before the user approves it; writes a review report. Use when the user wants a group reviewed.
argument-hint: [group | feature]
---

Do an expert review of an implementation group.

Group or feature: {{args}}

{{reviewer}}

The review:
1. Call the `review_context` tool with the group (and the feature, if one was given; with neither, it takes the group under review). If the tool is not available, run `sdd review [<group>] --context`.
2. Before the code, read what the brief lists under Standards: the language profile, the technical spec and the project guardrails. Review as a senior engineer in that language, by those rules.
3. Read the diff with the git command in the brief, then every changed file in full and the code it relies on.
4. Check correctness and edge cases; whether each covered criterion is really met and its tests would fail if it were not; error handling and security; the architecture; the profile and guardrails; needless complexity. Skip what a formatter or linter enforces.
5. Write the report where the brief says, in its format, with `file:line` for every finding.

Then give the user a short summary (recommendation, blocker and major findings, report path) and tell them the decision is theirs, as {{tell}}: approve the group, or revise it with a feedback message you draft from the blocker and major findings.

Do not change code, specs or the plan while reviewing, and never run `sdd approve` or `sdd revise`.
