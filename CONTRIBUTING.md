# Contributing

Thanks for considering a contribution to Pig.

Pig is source-available under a noncommercial license. By contributing, you agree that your contribution may be distributed under the project license.

Pig is a library-first Go port inspired by Pi and `c4pt0r/pie`. The project prioritizes embeddable runtime packages over product UI surfaces.

## Development

```bash
go test ./...
```

Before opening a pull request:

- Keep the change focused on one behavior or documentation improvement.
- Add or update tests for behavior changes.
- Avoid unrelated rewrites, formatting churn, or broad refactors.
- Do not add TUI, Web UI, OAuth, LSP, local-model autoloading, or slash-command UI wiring unless the project scope changes first.
- Do not commit API keys, session transcripts containing secrets, or local configuration files.

## Style

- Prefer small, explicit Go APIs that embedders can compose.
- Keep package boundaries clear: provider logic in `ai`, runtime loop behavior in `agent`, persistence in `session`/`sessionrunner`, and integration orchestration in `harness`.
- Use deterministic tests where possible. The faux provider is preferred for agent-loop tests that do not need a live LLM.

## Reporting Issues

Please include:

- Go version and operating system.
- The package or integration path involved.
- A minimal reproduction or failing test when possible.
- Whether live provider credentials are required to reproduce.
