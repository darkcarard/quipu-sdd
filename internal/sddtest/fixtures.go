// Package sddtest holds a valid feature (functional spec, technical spec and
// plan) shared by tests across packages. It is only imported from _test files.
package sddtest

// Feature is the name of the fixture feature.
const Feature = "user-signup"

// Functional is a functional.md that passes lint.
const Functional = `---
id: user-signup
title: User Signup
status: draft
version: 1
depends_on: []
---

# User Signup: Functional Specification

<!-- A comment with a <placeholder> and FR-999 that must be ignored. -->

## Summary

Visitors can create an account with an email address and a password.

## Problem & Goals

New users cannot register today. Goal: 90% of signups complete in under 1 minute.

## Non-Goals

- Social login.

## Users & Scenarios

- A visitor signs up from the landing page.

## Functional Requirements

### FR-001: Sign up with email
The system must create an account from an email and a password.

- **AC-001:** Given a new email, when the visitor submits the form, then an account is created.
- **AC-002:** Given an email that is already registered, when the visitor submits,
  then the signup is rejected with a clear message.

### FR-002: Confirm email
The system must send a confirmation link after signup (see AC-001).

- **AC-003:** Given a new account, when it is created, then a confirmation email is sent.

## Non-Functional Requirements

- **NFR-001:** Signup responds in p95 < 300 ms.

## Constraints

None.

## Open Questions

<!-- Must be empty before approval. -->

## Glossary

- **Visitor:** someone without an account.
`

// Technical is a technical.md consistent with Functional (functional_hash empty).
const Technical = `---
id: user-signup
title: User Signup
status: draft
version: 1
language: go
functional_hash: ""
commands:
  build: go build ./...
  lint: go vet ./...
  test: go test ./...
  integration: go test -tags=integration ./...
---

# User Signup: Technical Specification

## Overview

A signup service behind an HTTP handler.

## Architecture

domain → persistence → service → interface → integration

## Components

- SignupService (internal/signup)
- Mailer (internal/mail)

## Data Model

User{email, password_hash}.

## Interfaces / APIs

POST /signup.

## Requirement Traceability

| Requirement | Component(s) | Notes |
|---|---|---|
| FR-001 | SignupService, HTTP handler | |
| FR-002 | Mailer | |
| NFR-001 | HTTP handler | |

## Error Handling

Duplicate email returns 409.

## Testing Strategy

| Acceptance criterion | Level (unit / integration / e2e) | Test location |
|---|---|---|
| AC-001 | unit | internal/signup |
| AC-002 | unit, integration | internal/signup |
| AC-003 | integration | internal/mail |

## Decisions

### ADR-001: Store bcrypt hashes
- **Context:** passwords must not be stored in clear.
- **Decision:** bcrypt with cost 12.
- **Consequences:** slower logins.

## Risks

Email deliverability.
`

// Plan is a plan.md covering every FR and AC of Functional (hashes empty).
const Plan = `---
id: user-signup
title: User Signup
status: draft
version: 1
functional_hash: ""
technical_hash: ""
---

# User Signup: Implementation Plan

## G1: Domain model
- layer: domain
- depends_on: []
- goal: User entity with validation.

### T1.1: User entity
- covers: [FR-001, AC-001]
- files: [internal/signup/user.go, internal/signup/user_test.go]
- done_when: user validation tests pass
- verify: TestUser

Create the entity.

## G2: Signup service
- layer: service
- depends_on: [G1]
- goal: Service that registers users.

### T2.1: Register
- covers: [FR-001, AC-002, NFR-001]
- files: [internal/signup/service.go, internal/signup/service_test.go]
- done_when: duplicate emails are rejected
- verify: TestRegister

## G3: Confirmation mail
- layer: integration
- depends_on: [G2]
- goal: Send the confirmation email.

### T3.1: Confirm
- covers: [FR-002, AC-003]
- files: [internal/mail/mail.go, internal/mail/mail_test.go]
- done_when: the mail is sent on signup
- verify: TestConfirm
`
