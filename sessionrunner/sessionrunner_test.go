package sessionrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/skills"
)

func TestSessionRunnerRestoresContextAndPersistsSystemAssistantAndToolResult(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	model := ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")}
	var llmMessages []ai.Message
	runner := SessionRunner{
		Model:        model,
		SystemPrompt: "system from runner",
		Tools:        []agent.Tool{sessionRunnerFakeTool{}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			llmMessages = append([]ai.Message(nil), messages...)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, Delta: "think"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "calling"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentImage, Data: "aW1n", MimeType: "image/png"}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "fake", Arguments: map[string]any{"path": "README.md"}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventMetadata, ResponseID: "resp-1", ResponseModel: "served-model"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventUsage, Usage: &ai.Usage{InputTokens: 3, OutputTokens: 4, TotalTokenCount: 7, HasTotalTokens: true}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
		Config: agent.Config{ShouldStopAfterTurn: func(context.Context, agent.ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
	}

	out, err := runner.Run(context.Background(), RunSessionInput{SessionID: "sess-1", Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if out.State.ErrorMessage != "" {
		t.Fatalf("unexpected state error: %s", out.State.ErrorMessage)
	}
	if len(llmMessages) < 2 || llmMessages[0].Role != ai.RoleSystem || llmMessages[0].Content[0].Text != "system from runner" || llmMessages[1].Role != ai.RoleUser || llmMessages[1].Content[0].Text != "hello" {
		t.Fatalf("runner did not restore session context before LLM call: %#v", llmMessages)
	}

	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected user, system custom, assistant, tool result entries, got %d: %#v", len(entries), entries)
	}
	if entries[1].EntryType != session.EntryTypeCustom || entries[1].CustomType != "system_prompt" {
		t.Fatalf("system prompt should be custom entry, got %#v", entries[1])
	}
	assistantEntry := entries[2]
	if assistantEntry.EntryType != session.EntryTypeMessage || assistantEntry.Message == nil || assistantEntry.Message.LLM == nil {
		t.Fatalf("assistant entry missing: %#v", assistantEntry)
	}
	assistantMessage := assistantEntry.Message.LLM
	if assistantMessage.Role != ai.RoleAssistant || assistantMessage.API != model.API || assistantMessage.Provider != model.Provider || assistantMessage.Model != model.ID || assistantMessage.ResponseID != "resp-1" || assistantMessage.ResponseModel != "served-model" || assistantMessage.StopReason != ai.StopReasonToolCalls {
		t.Fatalf("assistant metadata not preserved: %#v", assistantMessage)
	}
	if len(assistantMessage.Content) != 4 || assistantMessage.Content[0].Type != ai.ContentThinking || assistantMessage.Content[1].Type != ai.ContentText || assistantMessage.Content[2].Type != ai.ContentImage || assistantMessage.Content[2].MimeType != "image/png" || assistantMessage.Content[3].Type != ai.ContentToolCall {
		t.Fatalf("assistant content blocks not preserved: %#v", assistantMessage.Content)
	}
	if len(assistantMessage.ToolCalls) != 1 || assistantMessage.ToolCalls[0].ID != "call-1" {
		t.Fatalf("assistant tool calls not preserved: %#v", assistantMessage.ToolCalls)
	}
	if assistantMessage.Usage == nil || assistantMessage.Usage.TotalTokenCount != 7 || !assistantMessage.Usage.HasTotalTokens {
		t.Fatalf("assistant usage not preserved: %#v", assistantMessage.Usage)
	}
	toolEntry := entries[3]
	if toolEntry.EntryType != session.EntryTypeMessage || toolEntry.Message == nil || toolEntry.Message.ToolResult == nil {
		t.Fatalf("tool result entry missing: %#v", toolEntry)
	}
	if toolEntry.Message.ToolResult.CallID != "call-1" || toolEntry.Message.ToolResult.Name != "fake" || toolEntry.Message.ToolResult.Content != "tool output" {
		t.Fatalf("tool result not preserved: %#v", toolEntry.Message.ToolResult)
	}
}

func TestSessionRunnerJSONLCreateReopenResumeRoundTripLikeUpstream(t *testing.T) {
	repo := session.NewJSONLRepo(t.TempDir())
	sess, err := repo.Create("/repo")
	if err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")}, Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ack"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}}
	if _, err := sess.AppendMessage(agent.NewUserMessage("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "jsonl-round-trip", Session: sess}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("second")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "jsonl-round-trip", Session: sess}); err != nil {
		t.Fatal(err)
	}

	files, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one jsonl session file, got %#v", files)
	}
	reopened, err := repo.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := reopened.BuildContext()
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, message := range ctx.Messages {
		if message.Kind != agent.MessageKindLLM || message.LLM == nil || len(message.LLM.Content) == 0 {
			continue
		}
		texts = append(texts, message.LLM.Content[0].Text)
	}
	if !reflect.DeepEqual(texts, []string{"first", "ack", "second", "ack"}) {
		t.Fatalf("reopened context mismatch: %#v", texts)
	}
}

func TestSessionRunnerUsesCodingAgentPromptAndTools(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	codingAgent := agent.NewCodingAgent(agent.CodingAgentOptions{
		Options:      agent.Options{Tools: []agent.Tool{sessionRunnerFakeTool{}}},
		Instructions: "Product instructions.",
		Skills:       []skills.Skill{{Name: "grilling", Description: "Interview", FilePath: "skill://grilling/SKILL.md", Content: "Ask one question."}},
	})
	var llmMessages []ai.Message
	var llmTools []ai.Tool
	runner := SessionRunner{
		Model:       ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		CodingAgent: codingAgent,
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			llmMessages = append([]ai.Message(nil), messages...)
			llmTools = append([]ai.Tool(nil), tools...)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: agent.Config{ShouldStopAfterTurn: func(context.Context, agent.ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "sess-1", Session: sess}); err != nil {
		t.Fatal(err)
	}
	if len(llmMessages) == 0 || llmMessages[0].Role != ai.RoleSystem {
		t.Fatalf("missing system prompt: %#v", llmMessages)
	}
	prompt := llmMessages[0].Content[0].Text
	if !strings.Contains(prompt, "Product instructions.") || !strings.Contains(prompt, "<name>grilling</name>") || !strings.Contains(prompt, "<name>fake</name>") || !strings.Contains(prompt, "<description>Fake tool</description>") {
		t.Fatalf("system prompt = %q", prompt)
	}
	if len(llmTools) != 2 || llmTools[0].Name != "fake" || llmTools[1].Name != "Skill" {
		t.Fatalf("tools = %#v", llmTools)
	}
}

func TestSessionRunnerWritesSystemPromptAsCustomJSONLEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess.jsonl")
	storage, err := session.CreateJSONLStorage(path, session.JSONLMetadata{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	sess := session.NewSession(storage)
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "system from runner",
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected metadata, user, system custom, assistant lines, got %d: %s", len(lines), raw)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["type"] != "custom" || entry["customType"] != "system_prompt" {
		t.Fatalf("system prompt should be custom JSONL entry, got %s", lines[2])
	}
	data := entry["data"].(map[string]any)
	message := data["message"].(map[string]any)
	if message["role"] != "system" {
		t.Fatalf("system prompt message not preserved: %s", lines[2])
	}
	if _, ok := entry["message"]; ok {
		t.Fatalf("system prompt custom entry should not contain root message field: %s", lines[2])
	}
}

func TestSessionRunnerPersistsSystemPromptAddedByTransformContext(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	transformSystem := ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "system from transform"}}}
	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Config: agent.Config{TransformContext: func(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
			return append([]ai.Message{transformSystem}, messages...), nil
		}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			if len(messages) == 0 || messages[0].Role != ai.RoleSystem || messages[0].Content[0].Text != "system from transform" {
				t.Fatalf("first LLM message = %#v", messages)
			}
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].EntryType != session.EntryTypeCustom || entries[1].CustomType != "system_prompt" {
		t.Fatalf("expected transformed system prompt custom entry, got %#v", entries)
	}
	data := entries[1].Data.(map[string]any)
	stored := data["message"].(ai.Message)
	if stored.Role != ai.RoleSystem || stored.Content[0].Text != "system from transform" {
		t.Fatalf("stored system prompt = %#v", stored)
	}
}

func TestSessionRunnerOutputContainsLastAssistantText(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "first"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentThinking, Thinking: "hidden"}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: " second"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	out, err := runner.Run(context.Background(), RunSessionInput{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Output) != "first second" {
		t.Fatalf("expected output to contain final assistant text, got %q", string(out.Output))
	}
}

func TestSessionRunnerPersistsToolResultBeforeNextLLMCall(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	streamCalls := 0
	var entriesBeforeSecondCall []session.Entry
	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []agent.Tool{sessionRunnerFakeTool{}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "fake"}})
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			var err error
			entriesBeforeSecondCall, err = sess.Entries()
			if err != nil {
				t.Fatal(err)
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if streamCalls != 2 {
		t.Fatalf("expected second LLM call after tool result, got %d", streamCalls)
	}
	if len(entriesBeforeSecondCall) != 3 {
		t.Fatalf("expected user, assistant tool call, and tool result persisted before second LLM call, got %#v", entriesBeforeSecondCall)
	}
	toolResult := entriesBeforeSecondCall[2].Message
	if toolResult == nil || toolResult.ToolResult == nil || toolResult.ToolResult.CallID != "call-1" {
		t.Fatalf("tool result was not persisted before second LLM call: %#v", entriesBeforeSecondCall)
	}
}

func TestSessionRunnerPersistsAssistantBeforeToolExecution(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	tool := sessionRunnerInspectingTool{inspect: func() {
		entries, err := sess.Entries()
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("expected user and assistant entries before tool execution, got %#v", entries)
		}
		assistant := entries[1].Message
		if assistant == nil || assistant.LLM == nil || len(assistant.LLM.ToolCalls) != 1 || assistant.LLM.ToolCalls[0].ID != "call-1" {
			t.Fatalf("assistant tool call was not persisted before tool execution: %#v", entries)
		}
	}}
	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []agent.Tool{tool},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "inspect"}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
		Config: agent.Config{ShouldStopAfterTurn: func(context.Context, agent.ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRunnerDoesNotDuplicateMultipleToolResults(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []agent.Tool{sessionRunnerFakeTool{}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "fake"}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-2", Name: "fake"}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
		Config: agent.Config{ShouldStopAfterTurn: func(context.Context, agent.ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	toolResults := 0
	for _, entry := range entries {
		if entry.Message != nil && entry.Message.ToolResult != nil {
			toolResults++
		}
	}
	if toolResults != 2 {
		t.Fatalf("expected exactly two tool result entries, got %d in %#v", toolResults, entries)
	}
}

func TestSessionRunnerUsesNormalizedToolResultCallIDForDedup(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []agent.Tool{sessionRunnerBareTool{}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bare"}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
		Config: agent.Config{ShouldStopAfterTurn: func(context.Context, agent.ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	toolResults := 0
	for _, entry := range entries {
		if entry.Message != nil && entry.Message.ToolResult != nil {
			toolResults++
			if entry.Message.ToolResult.CallID != "call-1" || entry.Message.ToolResult.Name != "bare" {
				t.Fatalf("tool result should be normalized with call id and name: %#v", entry.Message.ToolResult)
			}
		}
	}
	if toolResults != 1 {
		t.Fatalf("expected one normalized tool result entry, got %d in %#v", toolResults, entries)
	}
}

func TestSessionRunnerPersistsSystemPromptOnceAcrossToolLoop(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	streamCalls := 0
	runner := SessionRunner{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "system once",
		Tools:        []agent.Tool{sessionRunnerFakeTool{}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "fake"}})
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	systemPrompts := 0
	for _, entry := range entries {
		if entry.EntryType == session.EntryTypeCustom && entry.CustomType == "system_prompt" {
			systemPrompts++
		}
	}
	if systemPrompts != 1 {
		t.Fatalf("expected one system_prompt custom entry per run, got %d in %#v", systemPrompts, entries)
	}
}

func TestSessionRunnerPersistsUpdatedSystemPromptAcrossTurns(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	streamCalls := 0
	nextSystemPrompt := "next system"
	runner := SessionRunner{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "initial system",
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: agent.Config{PrepareNextTurn: func(ctx context.Context, turn agent.PrepareNextTurnContext) (*agent.AgentLoopTurnUpdate, error) {
			if streamCalls == 1 {
				return &agent.AgentLoopTurnUpdate{Messages: []agent.Message{agent.NewUserMessage("continue")}, SystemPrompt: &nextSystemPrompt}, nil
			}
			return nil, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, entry := range entries {
		if entry.EntryType != session.EntryTypeCustom || entry.CustomType != "system_prompt" {
			continue
		}
		data := entry.Data.(map[string]any)
		message := data["message"].(ai.Message)
		prompts = append(prompts, message.Content[0].Text)
	}
	if !reflect.DeepEqual(prompts, []string{"initial system", "next system"}) {
		t.Fatalf("expected both distinct system prompts, got %#v in %#v", prompts, entries)
	}
}

func TestSessionRunnerPersistsMessagesAddedDuringRun(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	streamCalls := 0
	followUp := agent.NewUserMessage("follow up")
	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "first"})
				stream.Close(ai.DoneReasonStop)
				return stream, nil
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "second"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: agent.Config{PrepareNextTurn: func(ctx context.Context, turn agent.PrepareNextTurnContext) (*agent.AgentLoopTurnUpdate, error) {
			if streamCalls == 1 {
				return &agent.AgentLoopTurnUpdate{Messages: []agent.Message{followUp}}, nil
			}
			return nil, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected initial user, first assistant, follow-up, second assistant entries, got %#v", entries)
	}
	if entries[2].Message == nil || entries[2].Message.LLM == nil || entries[2].Message.LLM.Role != ai.RoleUser || entries[2].Message.LLM.Content[0].Text != "follow up" {
		t.Fatalf("message added during run was not persisted: %#v", entries)
	}
}

func TestSessionRunnerReturnsErrorWhenPersistingAssistantFails(t *testing.T) {
	sess := session.NewSession(&sessionRunnerFailingStorage{Storage: session.NewMemorySessionStorage(), failOnAppend: 3})
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "system",
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist assistant message") {
		t.Fatalf("expected assistant persistence error, got %v", err)
	}
}

func TestSessionRunnerReturnsErrorWhenPersistingSystemPromptFails(t *testing.T) {
	sess := session.NewSession(&sessionRunnerFailingStorage{Storage: session.NewMemorySessionStorage(), failOnAppend: 2})
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	streamCalled := false
	runner := SessionRunner{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "system",
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalled = true
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist system prompt") {
		t.Fatalf("expected system prompt persistence error, got %v", err)
	}
	if streamCalled {
		t.Fatal("stream should not be called after system prompt persistence fails")
	}
}

func TestSessionRunnerReturnsErrorWhenPersistingToolResultFails(t *testing.T) {
	sess := session.NewSession(&sessionRunnerFailingStorage{Storage: session.NewMemorySessionStorage(), failOnAppend: 3})
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []agent.Tool{sessionRunnerFakeTool{}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "fake"}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist tool result") {
		t.Fatalf("expected tool result persistence error, got %v", err)
	}
}

func TestSessionRunnerReturnsErrorWhenPersistingMessageAddedDuringRunFails(t *testing.T) {
	sess := session.NewSession(&sessionRunnerFailingStorage{Storage: session.NewMemorySessionStorage(), failOnAppend: 3})
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	streamCalls := 0
	runner := SessionRunner{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "first"})
				stream.Close(ai.DoneReasonStop)
				return stream, nil
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "second"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: agent.Config{PrepareNextTurn: func(ctx context.Context, turn agent.PrepareNextTurnContext) (*agent.AgentLoopTurnUpdate, error) {
			if streamCalls == 1 {
				return &agent.AgentLoopTurnUpdate{Messages: []agent.Message{agent.NewUserMessage("follow up")}}, nil
			}
			return nil, nil
		}},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist session message") {
		t.Fatalf("expected added message persistence error, got %v", err)
	}
}

func TestSessionRunnerDoesNotPersistSystemPromptWhenNoModel(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{SystemPrompt: "system"}

	_, err := runner.Run(context.Background(), RunSessionInput{Session: sess})
	if err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatalf("expected no model error, got %v", err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].EntryType != session.EntryTypeMessage {
		t.Fatalf("system prompt should not be persisted without an LLM request, got %#v", entries)
	}
}

func TestSessionRunnerUsesSessionModelWhenRunnerModelIsEmpty(t *testing.T) {
	resetSessionRunnerModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "from-session", Provider: ai.Provider("test"), API: ai.ApiFaux})
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendModelChange("test", "from-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	var gotModel ai.Model
	runner := SessionRunner{
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotModel = model
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if gotModel.ID != "from-session" || gotModel.Provider != ai.Provider("test") || gotModel.API != ai.ApiFaux {
		t.Fatalf("expected session model, got %#v", gotModel)
	}
}

func TestSessionRunnerWithoutModelReturnsAgentNoModelError(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	streamCalls := 0
	runner := SessionRunner{Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}}

	out, err := runner.Run(context.Background(), RunSessionInput{Session: sess})
	if err == nil || err.Error() != "Agent has no model set; assign state.model first" {
		t.Fatalf("expected no model error, got err=%v out=%#v", err, out)
	}
	if streamCalls != 0 {
		t.Fatalf("stream should not be called without a model, got %d", streamCalls)
	}
}

func TestSessionRunnerPassesAPIKeyAndMirrorsEvents(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	var gotAPIKey string
	var eventTypes []agent.EventType
	runner := SessionRunner{
		Model:  ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		APIKey: "secret-key",
		Events: func(event agent.Event) { eventTypes = append(eventTypes, event.Type) },
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotAPIKey = options.Base.APIKey
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "secret-key" {
		t.Fatalf("expected API key to reach stream options, got %q", gotAPIKey)
	}
	if len(eventTypes) == 0 || eventTypes[0] != agent.EventTypeStart {
		t.Fatalf("expected mirrored agent events, got %#v", eventTypes)
	}
}

func TestSessionRunnerAPIKeyDoesNotOverrideConfigGetAPIKey(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	var gotAPIKey string
	runner := SessionRunner{
		Model:  ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		APIKey: "fallback-key",
		Config: agent.Config{GetAPIKey: func(context.Context, ai.Provider) (string, bool) {
			return "config-key", true
		}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotAPIKey = options.Base.APIKey
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "config-key" {
		t.Fatalf("expected Config.GetAPIKey to win over fallback APIKey, got %q", gotAPIKey)
	}
}

func TestSessionRunnerAPIKeyFallbackUsedWhenConfigGetAPIKeyMisses(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	var gotAPIKey string
	runner := SessionRunner{
		Model:  ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		APIKey: "fallback-key",
		Config: agent.Config{GetAPIKey: func(context.Context, ai.Provider) (string, bool) {
			return "", false
		}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotAPIKey = options.Base.APIKey
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "fallback-key" {
		t.Fatalf("expected runner APIKey fallback when Config.GetAPIKey misses, got %q", gotAPIKey)
	}
}

type sessionRunnerFakeTool struct{}

func (sessionRunnerFakeTool) Name() string        { return "fake" }
func (sessionRunnerFakeTool) Description() string { return "Fake tool" }
func (sessionRunnerFakeTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: "tool output", Details: map[string]any{"ok": true}}, nil
}

type sessionRunnerInspectingTool struct {
	inspect func()
}

func (sessionRunnerInspectingTool) Name() string        { return "inspect" }
func (sessionRunnerInspectingTool) Description() string { return "Inspect session" }
func (tool sessionRunnerInspectingTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	if tool.inspect != nil {
		tool.inspect()
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: "tool output"}, nil
}

type sessionRunnerBareTool struct{}

func (sessionRunnerBareTool) Name() string        { return "bare" }
func (sessionRunnerBareTool) Description() string { return "Bare tool" }
func (sessionRunnerBareTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	return agent.ToolResult{Content: "bare output"}, nil
}

type sessionRunnerFailingStorage struct {
	session.Storage
	appendCount  int
	failOnAppend int
}

func (storage *sessionRunnerFailingStorage) AppendEntry(entry session.Entry) error {
	storage.appendCount++
	if storage.appendCount == storage.failOnAppend {
		return errors.New("boom")
	}
	return storage.Storage.AppendEntry(entry)
}

func resetSessionRunnerModels(t *testing.T) {
	t.Helper()
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() {
		ai.ClearBuiltinModels()
		ai.ClearCustomModels()
	})
}
