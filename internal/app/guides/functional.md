# Drafting a functional spec

A functional spec says **what** the system must do and **why**, for one feature. It never says how: no frameworks, datastores, table names or code. Those belong in the technical spec.

## How to draft it

1. If the feature does not exist yet, create it with `spec_new`, passing the user's own words as `text` (sdd derives a valid feature name from them) and a one-line `description` if they gave one. Then read `specs/<feature>/functional.md`.
2. Interview the user before writing requirements: the problem and goals, who the users are and their main scenarios, what is explicitly out of scope, and any constraints. Ask a few questions at a time. Propose requirements and let the user confirm or correct them; do not invent scope.
3. Write the sections in the file as you go, so the draft is always on disk.
4. Anything still undecided goes under **Open Questions** as a bullet. Approval requires that section to be empty, so resolve each one with the user.
5. After every edit, call `lint` for the feature and fix every error. Fix warnings too, or tell the user why one should stay.
6. When lint passes and the user is satisfied, ask them to run `sdd spec approve <feature> --functional`. Never run it yourself.

## Format rules (checked by lint)

- The frontmatter `id` is the feature name. Keep the template's frontmatter.
- Required sections, in this order: Summary, Problem & Goals, Non-Goals, Users & Scenarios, Functional Requirements, Non-Functional Requirements, Constraints, Open Questions, Glossary.
- Each functional requirement is a level-3 heading followed by one testable statement using "must" or "should":

      ### FR-001: Sign up with email
      The system must create an account from an email and a password.

      - **AC-001:** Given a new email, when the visitor submits the form, then an account is created.
      - **AC-002:** Given an email that is already registered, when the visitor submits,
        then the signup is rejected with a clear message.

- Every FR has at least one acceptance criterion, written as Given / When / Then. An AC may continue on indented lines.
- Non-functional requirements are bullets under their section: `- **NFR-001:** Signup responds in p95 < 300 ms.` Make them measurable, with a number and a unit.
- IDs have three digits and are numbered in order of creation. FR, NFR and AC each have their own sequence across the whole feature (AC-003 may belong to FR-002).
- IDs are permanent. Never renumber or delete one: to drop a requirement, retire it as `### ~~FR-004~~ (removed)` and its criteria as `- ~~AC-007~~ (removed)`.
- Replace every template placeholder such as `<requirement title>`.
- Use the Glossary terms consistently.
