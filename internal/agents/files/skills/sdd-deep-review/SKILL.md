---
name: sdd-deep-review
description: Review whether an implemented feature really satisfies each acceptance criterion of its spec (sdd, Spec-Driven Development), and write a deep-review report. Use when the user asks for a semantic or deep review of a feature.
argument-hint: <feature>
---

Do a deep review of a feature.

Feature: {{args}}

Use the `sdd` MCP tools (if they are not available, run `sdd review <feature> --context` in the shell).

1. If no feature was given, call `status` and ask the user which feature to review.
2. Call `review_context` with the feature. It lists every acceptance criterion with the tasks, commits and tests linked to it, and the format of the report.
3. For each criterion, read the code and the tests it points to and decide whether the implementation actually satisfies it; look for criteria whose tests do not really verify them.
4. Write the report where `review_context` says, then summarize the findings for the user.

The review is informational: it never blocks a gate. Do not change code or specs while reviewing; propose changes to the user instead.
