# Pig vs Pie Library-Port Audit

This audit tracks the current scoped goal: port the core runtime ideas from `c4pt0r/pie` to Go as reusable library packages, not as a terminal/UI clone.

## Scope

In scope:

- AI/provider abstractions, model catalog, streaming events, tool calls, and message transforms.
- Agent loop, tools, subagents, tool results, lifecycle events, and continuation behavior.
- Session storage/replay, JSONL storage, branch/compaction summaries, and session-backed running.
- AgentHarness composition for system prompts, skills, templates, compaction, hooks, triggers, MCP, and turn-end continuation.
- Builtin coding tools, skills catalog/lifecycle tools, hooks, triggers/loops, MCP client/transports/adapters, cost, debug/diagnostic helpers, and library-facing package aliases.

Out of scope by user decision:

- Terminal TUI, Web UI/web relay, and web-layer implementation.
- OAuth login flows.
- LSP support.
- Local-model/DS4 autoloading and DS4-specific runtime support.
- Slash-command UI wiring.
- Session export/import as a required deliverable.
- Provider live-test binaries.
- Permission UX as a required deliverable.

## Evidence Matrix

| Area | Evidence | Status |
| --- | --- | --- |
| AI/provider abstractions | `ai` package tests cover provider registries, OpenAI Responses/Completions/Codex/Azure/OpenAI Chat, Anthropic, Mistral, Bedrock, Google/Vertex, Cloudflare helpers, event streams, retries, transforms, catalog loading. | Complete for scoped library goal |
| Agent loop | `agent` tests cover prompt/continue transcript behavior, tool calls/results, sequential/parallel tools, hooks, queues, abort, subagents, task tool, usage rollup. | Complete for scoped library goal |
| Session + runner | `session` and `sessionrunner` tests cover append/replay, JSONL repo/storage, context building, compaction entries, branch summaries, system prompt persistence, assistant/tool-result persistence before next LLM call, API-key/event mirroring. | Complete for scoped library goal |
| Harness composition | `harness` tests cover prompt/continue, session persistence, skills reload, templates, budget/cost, compaction, branch summaries, MCP loading, triggers, turn-end continuation, evaluator isolation. | Complete for scoped library goal |
| Skills/tools | `skills` and `tools` tests cover SKILL.md discovery/formatting/state, install/remove/build/list/set-state tools, read/write/edit/ls/find/grep/bash/git/web_fetch/web_search/task/memory behavior. | Complete for scoped library goal |
| MCP | `mcp` tests cover protocol encode/decode, client initialize/list/call/notifications, stdio/http transports, loader config, agent adapter, and MCP tool execution inside the agent loop. | Complete for scoped library goal |
| Triggers/loops | `triggers` and `harness` tests cover cron/file pollers, scheduled cron registry, dynamic triggers, notification hooks, dedup/cycle suppression, inject-summary/inject-and-run/subagent delivery, scheduled cron poll into AgentHarness. | Complete for scoped library goal |
| Compaction | `compaction` and `harness` tests cover token estimation, cut points, summarization, overflow retry, auto/manual harness compaction, and LLM-visible compaction summaries. | Complete for scoped library goal |
| Hooks/observability helpers | `hooks`, `debuglog`, `bugreport`, `cost`, `goal`, `inbox`, `otlp` tests cover payloads, command/webhook dispatch, summaries, reports, cost, goal state, inbox state, and OTLP helpers. | Complete for scoped library goal |
| Documentation | `README.md` states library scope, exclusions, implemented foundations, and integration coverage. | Complete |

## High-Value Integration Tests

These tests are the primary evidence that independent packages work together as a library runtime:

- `sessionrunner.TestSessionRunnerPersistsToolResultBeforeNextLLMCall`
- `mcp.TestMCPAdapterRunsInsideAgentLoop`
- `harness.TestAgentHarnessScheduledCronPollInjectsAndRequestsMainRun`
- `harness.TestAgentHarnessPromptCombinesSkillsAndAutoCompactionContext`
- `harness.TestAgentHarnessMoveToBranchSummaryIsVisibleToNextPrompt`
- `harness.TestAgentHarnessApplyLoadedMCPRegistersToolsHooksAndDirectInject`
- `triggers.TestJobRegistryBuildsPollersForSupervisor`
- `agent.TestAgentPromptWrappersMatchUpstreamFacade`

## Current Verification Gate

Run:

```bash
go test ./...
```

Current result: passing.

## Remaining Non-Goal Notes

Some upstream/product-facing items intentionally remain excluded or stub-compatible with upstream:

- `ai/images` image-generation registry remains a stub-compatible surface, matching upstream's current stub shape.
- CLI/TUI/web/OAuth/LSP/local-model/session-export/provider-live-test surfaces are not required for this scoped library port.
