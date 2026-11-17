# Contributing

Thanks for your interest in Quipu. Issues, ideas and test reports are welcome, especially from real runs with Codex CLI or Antigravity CLI (see [docs/E2E_TEST.md](docs/E2E_TEST.md)).

## Pull requests

- Open an issue first for anything beyond a small fix, so we can agree on the approach.
- `make check` must pass (gofmt, `go vet`, `go test -race`).
- Follow the project's conventions: Go code per [the Go profile](internal/templates/profiles/go.md) (table-driven tests, `t.TempDir()`, wrapped errors, `context.Context` first); docs, comments and CLI messages in English.
- When a change affects documented behavior, update the docs in the same pull request (the two manuals, `docs/MANUAL.html` and `docs/MANUAL.es.html`, are kept in sync).
- Keep dependencies minimal: the standard library, `gopkg.in/yaml.v3` and the official MCP Go SDK.
- `sdd` never calls an LLM, and approvals stay human-only: changes that break either are not accepted (see [docs/BRIEF.md](docs/BRIEF.md)).

`main` is protected: changes from contributors arrive through reviewed pull requests.

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).

For security problems, see [SECURITY.md](SECURITY.md).
