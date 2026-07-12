# Pig

[![CI](https://github.com/detailyang/pig/actions/workflows/ci.yml/badge.svg)](https://github.com/detailyang/pig/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/detailyang/pig.svg)](https://pkg.go.dev/github.com/detailyang/pig)
[![Go Report Card](https://goreportcard.com/badge/github.com/detailyang/pig)](https://goreportcard.com/report/github.com/detailyang/pig)

Pig is a Go library for building coding-agent runtimes. It is inspired by [Pi](https://pi.dev/) and the core runtime ideas in `c4pt0r/pie`, but it is intentionally focused on embeddable Go packages instead of a terminal UI clone.

> Project status: early source-available, noncommercial library. APIs are useful for experimentation and embedding, but may still change before a stable `v1` release.

## Why Pig

- **Library first**: compose an agent runtime from Go packages instead of shelling out to a TUI app.
- **Provider aware**: route messages, tools, streaming events, and model metadata across supported LLM APIs.
- **Session backed**: persist append-only JSONL transcripts and replay them into future turns.
- **Tool ready**: expose coding tools, skill tools, MCP adapters, hooks, triggers, and compaction as library surfaces.
- **Testable by design**: use the built-in faux provider to test agent loops without live API keys.

## Scope

Pig currently targets the reusable runtime layer:

- `ai`: provider registry, model catalog, message/content/tool-call types, streaming event aggregation, retries, and cross-provider transforms.
- `agent`: agent state, prompt/continue loops, tool execution, subagents, lifecycle events, and usage rollups.
- `session` and `sessionrunner`: append-only sessions, JSONL storage, context replay, branch summaries, compaction entries, and session-backed runs.
- `tools`: read/write/edit/ls/find/grep/bash/git/web/task tools plus memory and skill lifecycle helpers.
- `skills`: `SKILL.md` discovery, frontmatter parsing, invocation blocks, state overlays, reloadable catalogs, and audit entries.
- `compaction`: context-window estimation, cut-point selection, summarization, and storage rewrite support.
- `hooks`, `triggers`, and `mcp`: runtime hooks, cron/file/injected triggers, MCP clients/transports, and agent-tool adapters.
- `cost`, `debuglog`, `bugreport`, `goal`, `inbox`, and related support packages for embedders.

Pig intentionally does not try to clone every product surface. Terminal TUI, Web UI/web relay, OAuth login flows, LSP integration, local-model/DS4 autoloading, slash-command UI wiring, session export/import as a required runtime goal, provider live-test binaries, and permission UX are outside the current scope.

## Install

```bash
go get github.com/detailyang/pig
```

Pig currently requires the Go version declared in `go.mod`.

## Quick Start

Run a minimal agent with the deterministic faux provider:

```go
package main

import (
    "context"
    "fmt"

    "github.com/detailyang/pig/agent"
    "github.com/detailyang/pig/ai"
)

func main() {
    ai.SetFauxResponses([]ai.AssistantMessage{
        ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("hello from pig")}),
    })
    defer ai.ClearFauxResponses()

    model := ai.Model{ID: "faux", Provider: ai.Provider("faux"), API: ai.ApiFaux}
    runtime := agent.New(agent.Options{
        Model:  model,
        Stream: agent.DefaultStreamFn(),
    })

    state, err := runtime.Run(context.Background(), []agent.Message{
        agent.NewUserMessage("Say hello"),
    })
    if err != nil {
        panic(err)
    }

    for _, message := range state.Messages {
        if message.LLM == nil || message.LLM.Role != ai.RoleAssistant {
            continue
        }
        for _, block := range message.LLM.Content {
            if block.Type == ai.ContentText {
                fmt.Println(block.Text)
            }
        }
    }
}
```

A runnable version lives in `examples/faux_agent`:

```bash
go run ./examples/faux_agent
```

For session-backed embedding, use `sessionrunner.SessionRunner` so the JSONL transcript remains the authoritative conversation store while the agent is running.

## OpenTelemetry

Pig can emit `invoke_agent`, `chat`, and `execute_tool` spans through an OpenTelemetry tracer provider configured by the embedding application:

```go
runtime := agent.New(agent.Options{
    Model: model,
    Stream: agent.DefaultStreamFn(),
    OpenTelemetry: &agent.OpenTelemetryOptions{
        TracerProvider: tracerProvider,
    },
})
```

If `TracerProvider` is omitted, Pig uses the global provider from `otel.SetTracerProvider`. The application remains responsible for configuring its SDK/exporter and shutting the provider down. Model and tool inputs and outputs are excluded by default; set `RecordInputs` or `RecordOutputs` only when that data is safe to export.

## Verification

```bash
go test ./...
```

The current library-port coverage and integration evidence are tracked in `docs/pie-port-audit.md`.

## Contributing

Contributions are welcome. Please read `CONTRIBUTING.md` before sending larger changes. Keep changes small, tested, and aligned with the library-first scope.

## Security

Please do not file public issues for vulnerabilities. See `SECURITY.md` for the preferred reporting process.

## License

Pig is source-available for noncommercial use. It is not licensed under an OSI-approved open-source license.

Pig is released under the PolyForm Noncommercial License 1.0.0. Commercial use is prohibited unless you have separate written permission. See `LICENSE`.
