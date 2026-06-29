package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type SessionState = agent.State

type SessionRunner struct {
	Model         ai.Model
	APIKey        string
	SystemPrompt  string
	Tools         []agent.Tool
	Stream        agent.StreamFunc
	Events        agent.Listener
	StreamOptions ai.SimpleStreamOptions
	Config        agent.Config
	ToolExecution agent.ToolExecutionMode
}

type RunSessionInput struct {
	SessionID string
	Session   *Session
}

type RunSessionOutput struct {
	State         SessionState
	WaitingPrompt string
	Output        []byte
	Error         string
}

func (runner SessionRunner) Run(ctx context.Context, input RunSessionInput) (RunSessionOutput, error) {
	if input.Session == nil {
		return RunSessionOutput{}, fmt.Errorf("session runner requires session")
	}
	sessionContext, err := input.Session.BuildContext()
	if err != nil {
		return RunSessionOutput{}, err
	}
	activeEntries, err := input.Session.Branch(nil)
	if err != nil {
		return RunSessionOutput{}, err
	}

	model := runner.Model
	hasModel := !isZeroSessionRunnerModel(model)
	if isZeroSessionRunnerModel(model) && sessionContext.Model != nil {
		provider := ai.Provider(sessionContext.Model.Provider)
		if catalogModel, ok := ai.GetModel(provider, sessionContext.Model.ModelID); ok {
			model = catalogModel
		} else {
			model = ai.Model{Provider: provider, ID: sessionContext.Model.ModelID}
		}
		hasModel = true
	}
	state := agent.State{SystemPrompt: runner.SystemPrompt, Tools: append([]agent.Tool(nil), runner.Tools...), Messages: append([]agent.Message(nil), sessionContext.Messages...)}
	if hasModel {
		state.Model = &model
	}
	if thinkingLevel, ok := sessionRunnerThinkingLevel(sessionContext.ThinkingLevel); ok {
		state.ThinkingLevel = &thinkingLevel
	}

	stream := runner.Stream
	if stream == nil {
		stream = agent.DefaultStreamFn()
	}

	config := runner.Config
	if runner.APIKey != "" {
		originalGetAPIKey := config.GetAPIKey
		config.GetAPIKey = func(ctx context.Context, provider ai.Provider) (string, bool) {
			if originalGetAPIKey != nil {
				if apiKey, ok := originalGetAPIKey(ctx, provider); ok {
					return apiKey, true
				}
			}
			return runner.APIKey, true
		}
	}

	options := agent.Options{InitialState: &state, SystemPrompt: runner.SystemPrompt, SessionID: input.SessionID, Tools: runner.Tools, Stream: stream, StreamOptions: runner.StreamOptions, Config: config, ToolExecution: runner.ToolExecution}
	if hasModel {
		options.Model = model
	}
	agentRunner := agent.New(options)
	persist := newSessionRunnerPersister(input.Session, activeEntries)
	unsubscribe := agentRunner.Subscribe(func(event agent.Event) {
		if persist.err != nil {
			return
		}
		if runner.Events != nil {
			runner.Events(event)
		}
		persist.handleEvent(event)
		if persist.err != nil {
			agentRunner.Abort()
			panic(persist.err)
		}
	})
	defer unsubscribe()

	var finalState agent.State
	var runErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				if persist.err != nil {
					finalState = agentRunner.State()
					return
				}
				panic(recovered)
			}
		}()
		finalState, runErr = agentRunner.Continue(ctx)
	}()
	output := RunSessionOutput{State: finalState, Output: sessionRunnerOutput(sessionContext.Messages, finalState.Messages)}
	if persist.err != nil {
		output.Error = persist.err.Error()
		return output, persist.err
	}
	if runErr != nil {
		output.Error = runErr.Error()
		return output, runErr
	}
	return output, nil
}

type sessionRunnerPersister struct {
	session            *Session
	err                error
	systemPrompts      map[string]bool
	persistedMessages  map[*agent.Message]bool
	toolResultMessages map[string]*agent.Message
}

func newSessionRunnerPersister(session *Session, entries []Entry) *sessionRunnerPersister {
	persist := &sessionRunnerPersister{session: session}
	for _, entry := range entries {
		if entry.EntryType != EntryTypeCustom || entry.CustomType != "system_prompt" {
			continue
		}
		if message, ok := sessionRunnerSystemPromptFromData(entry.Data); ok {
			persist.markSystemPrompt(message)
		}
	}
	return persist
}

func sessionRunnerSystemPromptFromData(data any) (ai.Message, bool) {
	object, ok := data.(map[string]any)
	if !ok {
		return ai.Message{}, false
	}
	messageValue, ok := object["message"]
	if !ok {
		return ai.Message{}, false
	}
	if message, ok := messageValue.(ai.Message); ok {
		return message, true
	}
	encoded, err := json.Marshal(messageValue)
	if err != nil {
		return ai.Message{}, false
	}
	var message ai.Message
	if err := json.Unmarshal(encoded, &message); err != nil {
		return ai.Message{}, false
	}
	return message, true
}

func (persist *sessionRunnerPersister) handleEvent(event agent.Event) {
	var err error
	switch event.Type {
	case agent.EventTypeMessageStart:
		persist.rememberMessage(event.Message)
	case agent.EventTypeMessageEnd:
		if persist.shouldPersistMessage(event.Message) {
			_, err = persist.session.AppendMessage(*event.Message)
			persist.markMessagePersisted(event.Message)
		}
	case agent.EventTypeSystemPrompt:
		if event.LLMMessage != nil && persist.markSystemPrompt(*event.LLMMessage) {
			_, err = persist.session.AppendSystemPrompt(*event.LLMMessage)
		}
	case agent.EventTypeAssistant:
		if event.Message != nil {
			_, err = persist.session.AppendMessage(*event.Message)
			persist.markMessagePersisted(event.Message)
		}
	case agent.EventTypeToolResult:
		if event.ToolResult != nil {
			_, err = persist.session.AppendMessage(agent.NewToolResultMessage(*event.ToolResult))
			persist.markToolResultPersisted(event.ToolResult.CallID)
		}
	}
	if err != nil {
		persist.err = sessionRunnerPersistError(event.Type, err)
	}
}

func (persist *sessionRunnerPersister) rememberMessage(message *agent.Message) {
	if message == nil || message.ToolResult == nil || message.ToolResult.CallID == "" {
		return
	}
	if persist.toolResultMessages == nil {
		persist.toolResultMessages = map[string]*agent.Message{}
	}
	persist.toolResultMessages[message.ToolResult.CallID] = message
}

func (persist *sessionRunnerPersister) shouldPersistMessage(message *agent.Message) bool {
	if message == nil || persist.persistedMessages[message] {
		return false
	}
	return true
}

func (persist *sessionRunnerPersister) markMessagePersisted(message *agent.Message) {
	if message == nil {
		return
	}
	if persist.persistedMessages == nil {
		persist.persistedMessages = map[*agent.Message]bool{}
	}
	persist.persistedMessages[message] = true
}

func (persist *sessionRunnerPersister) markToolResultPersisted(callID string) {
	message := persist.toolResultMessages[callID]
	if message == nil {
		return
	}
	persist.markMessagePersisted(message)
}

func (persist *sessionRunnerPersister) markSystemPrompt(message ai.Message) bool {
	key := sessionRunnerSystemPromptKey(message)
	if persist.systemPrompts == nil {
		persist.systemPrompts = map[string]bool{}
	}
	if persist.systemPrompts[key] {
		return false
	}
	persist.systemPrompts[key] = true
	return true
}

func sessionRunnerSystemPromptKey(message ai.Message) string {
	data, err := marshalJSONNoHTMLEscape(message)
	if err != nil {
		return ai.AssistantMessage{Content: message.Content}.Text()
	}
	return string(data)
}

func sessionRunnerPersistError(eventType agent.EventType, err error) error {
	switch eventType {
	case agent.EventTypeMessageEnd:
		return fmt.Errorf("persist session message: %w", err)
	case agent.EventTypeSystemPrompt:
		return fmt.Errorf("persist system prompt: %w", err)
	case agent.EventTypeAssistant:
		return fmt.Errorf("persist assistant message: %w", err)
	case agent.EventTypeToolResult:
		return fmt.Errorf("persist tool result: %w", err)
	default:
		return fmt.Errorf("persist session event %s: %w", eventType, err)
	}
}

func sessionRunnerThinkingLevel(level string) (ai.ThinkingLevel, bool) {
	switch agent.ThinkingLevel(level) {
	case agent.ThinkingMinimal:
		return ai.ThinkingMinimal, true
	case agent.ThinkingLow:
		return ai.ThinkingLow, true
	case agent.ThinkingMedium:
		return ai.ThinkingMedium, true
	case agent.ThinkingHigh:
		return ai.ThinkingHigh, true
	case agent.ThinkingXHigh:
		return ai.ThinkingXHigh, true
	default:
		return "", false
	}
}

func sessionRunnerOutput(previous, current []agent.Message) []byte {
	if len(current) <= len(previous) {
		return nil
	}
	var buffer bytes.Buffer
	for _, message := range current[len(previous):] {
		switch message.Kind {
		case agent.MessageKindLLM:
			if message.LLM != nil && message.LLM.Role == ai.RoleAssistant {
				writeSessionRunnerContent(&buffer, message.LLM.Content)
			}
		case agent.MessageKindToolResult:
			if message.ToolResult != nil {
				writeSessionRunnerContent(&buffer, message.ToolResult.ContentBlocks)
				if len(message.ToolResult.ContentBlocks) == 0 && message.ToolResult.Content != "" {
					if buffer.Len() > 0 {
						buffer.WriteByte('\n')
					}
					buffer.WriteString(message.ToolResult.Content)
				}
			}
		}
	}
	return buffer.Bytes()
}

func writeSessionRunnerContent(buffer *bytes.Buffer, blocks []ai.ContentBlock) {
	for _, block := range blocks {
		if block.Type != ai.ContentText || block.Text == "" {
			continue
		}
		if buffer.Len() > 0 {
			buffer.WriteByte('\n')
		}
		buffer.WriteString(block.Text)
	}
}

func isZeroSessionRunnerModel(model ai.Model) bool {
	return model.ID == "" && model.API == "" && model.Provider == ""
}
