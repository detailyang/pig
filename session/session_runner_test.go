package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func TestSessionRunnerRestoresSessionAndPersistsSystemAssistantAndToolResult(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendThinkingLevelChange("high"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendModelChange("faux", "session-model"); err != nil {
		t.Fatal(err)
	}

	seenMessages := 0
	seenThinking := ai.ThinkingLevel("")
	toolCall := ai.ToolCall{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "ok"}}
	runner := SessionRunner{
		Model:        ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		SystemPrompt: "real system",
		Tools:        []agent.Tool{sessionRunnerEchoTool{}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seenMessages = len(messages)
			seenThinking = options.ThinkingLevel
			if len(messages) == 0 || messages[0].Role != ai.RoleSystem || messages[0].Content[0].Text != "real system" {
				t.Fatalf("stream should receive real system prompt first: %#v", messages)
			}
			if len(messages) < 2 || messages[1].Role != ai.RoleUser || messages[1].Content[0].Text != "hello" {
				t.Fatalf("stream should receive restored session user message: %#v", messages)
			}
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{
				{Type: ai.ContentThinking, Thinking: "plan"},
				{Type: ai.ContentText, Text: "use tool"},
				{Type: ai.ContentToolCall, ToolCall: &toolCall},
			}))
			return stream, nil
		},
		Config: agent.Config{ShouldStopAfterTurn: func(context.Context, agent.ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
	}

	output, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if output.Error != "" || !strings.Contains(string(output.Output), "echo:ok") {
		t.Fatalf("unexpected output: %#v", output)
	}
	if seenMessages != 2 || seenThinking != ai.ThinkingHigh {
		t.Fatalf("runner did not restore context/thinking: messages=%d thinking=%q", seenMessages, seenThinking)
	}

	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("expected original 3 entries plus system, assistant, tool result; got %d %#v", len(entries), entries)
	}
	if entries[3].EntryType != EntryTypeCustom || entries[3].CustomType != "system_prompt" {
		t.Fatalf("system prompt should be custom entry, got %#v", entries[3])
	}
	data, err := json.Marshal(entries[3])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"type":"message"`) || !strings.Contains(string(data), `"customType":"system_prompt"`) {
		t.Fatalf("system prompt should serialize as custom entry: %s", data)
	}
	assistantEntry := entries[4]
	if assistantEntry.Message == nil || assistantEntry.Message.LLM == nil {
		t.Fatalf("assistant entry missing message: %#v", assistantEntry)
	}
	assistantMessage := assistantEntry.Message.LLM
	if assistantMessage.Content[0].Type != ai.ContentThinking || assistantMessage.Content[1].Text != "use tool" || len(assistantMessage.ToolCalls) != 1 || assistantMessage.ToolCalls[0].ID != "call-1" || assistantMessage.Provider != ai.Provider("faux") || assistantMessage.Model != "runner-model" {
		t.Fatalf("assistant metadata/content/tool call not preserved: %#v", assistantMessage)
	}
	toolEntry := entries[5]
	if toolEntry.Message == nil || toolEntry.Message.ToolResult == nil {
		t.Fatalf("tool result entry missing message: %#v", toolEntry)
	}
	if toolEntry.Message.ToolResult.CallID != "call-1" || toolEntry.Message.ToolResult.Name != "echo" || toolEntry.Message.ToolResult.Content != "echo:ok" || toolEntry.Message.ToolResult.Details["source"] != "test" {
		t.Fatalf("tool result not preserved: %#v", toolEntry.Message.ToolResult)
	}
}

func TestSessionRunnerRestoresModelAPIFromCatalogWhenOnlySessionModelExists(t *testing.T) {
	resetSessionRunnerModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "session-model", Provider: ai.Provider("faux"), API: ai.ApiFaux})
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendModelChange("faux", "session-model"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	var gotModel ai.Model
	runner := SessionRunner{Stream: func(_ context.Context, model ai.Model, messages []ai.Message, _ []ai.Tool, _ ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		gotModel = model
		if len(messages) != 1 || messages[0].Role != ai.RoleUser || messages[0].Content[0].Text != "hello" {
			t.Fatalf("expected restored user message, got %#v", messages)
		}
		stream := ai.NewAssistantMessageEventStream()
		ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("answer")}))
		return stream, nil
	}}

	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess}); err != nil {
		t.Fatal(err)
	}
	if gotModel.Provider != ai.Provider("faux") || gotModel.ID != "session-model" || gotModel.API != ai.ApiFaux {
		t.Fatalf("session model was not restored through catalog: %#v", gotModel)
	}
}

func TestSessionRunnerMirrorsEvents(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendMessage(agent.NewUserMessage("hi")); err != nil {
		t.Fatal(err)
	}
	var eventTypes []agent.EventType
	runner := SessionRunner{
		Model:        ai.Model{ID: "test", Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses},
		SystemPrompt: "system",
		Events: func(event agent.Event) {
			eventTypes = append(eventTypes, event.Type)
		},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventStart})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "hello"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess}); err != nil {
		t.Fatal(err)
	}
	if len(eventTypes) == 0 {
		t.Fatal("expected mirrored events")
	}
}

func TestSessionRunnerPersistsSystemPromptAddedByTransformContext(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	transformSystem := ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "system from transform"}}}
	runner := SessionRunner{
		Model: ai.Model{ID: "runner-model", Provider: ai.Provider("faux"), API: ai.ApiFaux},
		Config: agent.Config{TransformContext: func(_ context.Context, messages []ai.Message) ([]ai.Message, error) {
			return append([]ai.Message{transformSystem}, messages...), nil
		}},
		Stream: func(_ context.Context, _ ai.Model, messages []ai.Message, _ []ai.Tool, _ ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			if len(messages) == 0 || messages[0].Role != ai.RoleSystem || messages[0].Content[0].Text != "system from transform" {
				t.Fatalf("first LLM message = %#v", messages)
			}
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("answer")}))
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].EntryType != EntryTypeCustom || entries[1].CustomType != "system_prompt" {
		t.Fatalf("expected transformed system prompt custom entry, got %#v", entries)
	}
	data := entries[1].Data.(map[string]any)
	stored := data["message"].(ai.Message)
	if stored.Role != ai.RoleSystem || stored.Content[0].Text != "system from transform" {
		t.Fatalf("stored system prompt = %#v", stored)
	}
}

func TestSessionRunnerReturnsPersistError(t *testing.T) {
	storage := NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"})
	sess := NewSession(storage)
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	sess = NewSession(&failingAppendStorage{Storage: storage})
	runner := SessionRunner{
		Model:        ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		SystemPrompt: "real system",
		Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("never")}))
			return stream, nil
		},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist system prompt") {
		t.Fatalf("expected persist error, got %v", err)
	}
}

func TestSessionRunnerPersistsMessagesAddedDuringRun(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	streamCalls := 0
	followUp := agent.NewUserMessage("follow up")
	runner := SessionRunner{
		Model: ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("first")}))
				return stream, nil
			}
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("second")}))
			return stream, nil
		},
		Config: agent.Config{PrepareNextTurn: func(context.Context, agent.PrepareNextTurnContext) (*agent.AgentLoopTurnUpdate, error) {
			if streamCalls == 1 {
				return &agent.AgentLoopTurnUpdate{Messages: []agent.Message{followUp}}, nil
			}
			return nil, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected user, first assistant, follow-up, second assistant entries, got %#v", entries)
	}
	if entries[2].Message == nil || !reflect.DeepEqual(*entries[2].Message, followUp) {
		t.Fatalf("follow-up message was not persisted: %#v", entries)
	}
}

func TestSessionRunnerPersistsUpdatedSystemPromptAcrossTurns(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}

	streamCalls := 0
	nextSystemPrompt := "next system"
	runner := SessionRunner{
		Model:        ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		SystemPrompt: "initial system",
		Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("turn")}))
			return stream, nil
		},
		Config: agent.Config{PrepareNextTurn: func(context.Context, agent.PrepareNextTurnContext) (*agent.AgentLoopTurnUpdate, error) {
			if streamCalls == 1 {
				return &agent.AgentLoopTurnUpdate{Messages: []agent.Message{agent.NewUserMessage("continue")}, SystemPrompt: &nextSystemPrompt}, nil
			}
			return nil, nil
		}},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, entry := range entries {
		if entry.EntryType != EntryTypeCustom || entry.CustomType != "system_prompt" {
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

func TestSessionRunnerDoesNotDuplicateExistingSystemPromptOnRerun(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	existingPrompt := ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "real system"}}}
	if _, err := sess.AppendSystemPrompt(existingPrompt); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{
		Model:        ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		SystemPrompt: "real system",
		Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("answer")}))
			return stream, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess}); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	promptCount := 0
	for _, entry := range entries {
		if entry.EntryType == EntryTypeCustom && entry.CustomType == "system_prompt" {
			promptCount++
		}
	}
	if promptCount != 1 {
		t.Fatalf("system prompt should be idempotent across reruns, got %d prompts in %#v", promptCount, entries)
	}
}

func TestSessionRunnerSystemPromptKeyDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	message := ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "<system>&prompt"}}}
	key := sessionRunnerSystemPromptKey(message)
	if strings.Contains(key, `\u003c`) || strings.Contains(key, `\u003e`) || strings.Contains(key, `\u0026`) {
		t.Fatalf("system prompt key should not HTML-escape strings like upstream serde_json: %s", key)
	}
	if !strings.Contains(key, `"text":"<system>&prompt"`) {
		t.Fatalf("system prompt key missing unescaped prompt text: %s", key)
	}
}

func TestSessionRunnerReturnsErrorWhenPersistingAssistantFails(t *testing.T) {
	sess := NewSession(&countingFailStorage{Storage: NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}), failOnAppend: 3})
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{
		Model:        ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		SystemPrompt: "real system",
		Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("answer")}))
			return stream, nil
		},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist assistant message") {
		t.Fatalf("expected assistant persistence error, got %v", err)
	}
}

func TestSessionRunnerReturnsErrorWhenPersistingSystemPromptFails(t *testing.T) {
	sess := NewSession(&countingFailStorage{Storage: NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}), failOnAppend: 2})
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{
		Model:        ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		SystemPrompt: "real system",
		Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("answer")}))
			return stream, nil
		},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist system prompt") {
		t.Fatalf("expected system prompt persistence error, got %v", err)
	}
}

func TestSessionRunnerReturnsErrorWhenPersistingToolResultFails(t *testing.T) {
	sess := NewSession(&countingFailStorage{Storage: NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}), failOnAppend: 3})
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	runner := SessionRunner{
		Model: ai.Model{ID: "runner-model", API: ai.ApiFaux, Provider: ai.Provider("faux")},
		Tools: []agent.Tool{sessionRunnerEchoTool{}},
		Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "ok"}}}}))
			return stream, nil
		},
	}

	_, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess})
	if err == nil || !strings.Contains(err.Error(), "persist tool result") {
		t.Fatalf("expected tool result persistence error, got %v", err)
	}
}

func TestSessionRunnerWithoutModelReturnsAgentNoModelError(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "runner", CreatedAt: "now"}))
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	streamCalls := 0
	runner := SessionRunner{Stream: func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}}

	out, err := runner.Run(context.Background(), RunSessionInput{SessionID: "runner", Session: sess})
	if err == nil || err.Error() != "Agent has no model set; assign state.model first" {
		t.Fatalf("expected no model error, got err=%v out=%#v", err, out)
	}
	if streamCalls != 0 {
		t.Fatalf("stream should not be called without a model, got %d", streamCalls)
	}
}

type sessionRunnerEchoTool struct{}

func (sessionRunnerEchoTool) Name() string { return "echo" }

func (sessionRunnerEchoTool) Description() string { return "echo input" }

func (sessionRunnerEchoTool) Execute(_ context.Context, call ai.ToolCall, _ agent.ToolUpdateFunc) (agent.ToolResult, error) {
	text, _ := call.Arguments["text"].(string)
	return agent.ToolResult{Content: "echo:" + text, Details: map[string]any{"source": "test"}}, nil
}

type failingAppendStorage struct {
	Storage
}

func (storage *failingAppendStorage) AppendEntry(Entry) error {
	return errors.New("append failed")
}

type countingFailStorage struct {
	Storage
	appendCount  int
	failOnAppend int
}

func (storage *countingFailStorage) AppendEntry(entry Entry) error {
	storage.appendCount++
	if storage.appendCount == storage.failOnAppend {
		return errors.New("append failed")
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
