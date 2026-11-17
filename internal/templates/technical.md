---
id: {{feature}}
title: {{title}}
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

# {{title}}: Technical Specification

<!--
Describe HOW the approved functional spec will be built.
Every FR/NFR must appear in Requirement Traceability, and every AC in Testing Strategy.
-->

## Overview

<!-- The technical approach in a few sentences. -->

## Architecture

<!-- Layers (e.g. domain → persistence → service → interface) and how they interact. -->

## Components

<!-- Each component: responsibility, package/module, dependencies. -->

## Data Model

<!-- Entities, fields, invariants, storage. -->

## Interfaces / APIs

<!-- Public APIs, CLI commands, events, contracts with other features. -->

## Requirement Traceability

| Requirement | Component(s) | Notes |
|---|---|---|
| FR-001 | <component> | |
| NFR-001 | <component> | |

## Error Handling

<!-- Error categories, propagation, user-facing messages, retries. -->

## Testing Strategy

| Acceptance criterion | Level (unit / integration / e2e) | Test location |
|---|---|---|
| AC-001 | unit | <package or file> |

## Decisions

### ADR-001: <decision title>
- **Context:** <forces at play>
- **Decision:** <what was decided>
- **Consequences:** <trade-offs accepted>

## Risks

<!-- Technical risks and how they are mitigated. -->
