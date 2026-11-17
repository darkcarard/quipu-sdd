---
language: go
commands:
  build: go build ./...
  lint: go vet ./...
  test: go test ./...
  integration: go test -tags=integration ./...
---

# Go Best-Practice Profile

sdd hands this profile to the agent with every task of the Implement phase, for projects whose technical spec declares `language: go`.

## Code
- Format with `gofmt` and organize imports with `goimports`. Code that is not gofmt-clean is not done.
- Keep packages small and cohesive. Name each package after what it provides, not after `util`, `common` or `helpers`.
- Accept interfaces and return concrete types. Define interfaces where they are consumed.
- Pass `context.Context` as the first parameter of anything that does I/O or may block. Never store it in a struct.
- Return errors; do not panic. Wrap errors with `fmt.Errorf("doing x: %w", err)`, and use `errors.Is` and `errors.As` to inspect them.
- Make zero values useful where practical. Avoid global mutable state.
- Each goroutine needs a clear owner and a clear way to stop. Guard shared state with a mutex, or confine it to a single goroutine.
- Export only what other packages need, and document every exported identifier.

## Tests
- Use table-driven tests with `t.Run` subtests. Use `t.TempDir()` and `t.Setenv()` for isolation.
- Unit tests live next to the code (`x_test.go`). Integration tests use the `integration` build tag.
- Reference acceptance criteria with a `// covers: AC-###` comment on the test.
- Keep tests deterministic: no sleeps, no real network, and inject clocks and randomness.

## Before Finishing a Group
- `go build ./...`, `go vet ./...` and `go test ./...` must all pass.
- Run `go mod tidy` if you added or removed dependencies.
