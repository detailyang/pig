package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/detailyang/pig/ai"
)

type Listener func(Event)

type AgentListener = Listener

type ActiveDoneListener func(Event, <-chan struct{})

type Options struct {
	InitialState  *State
	Model         ai.Model
	SystemPrompt  string
	SessionID     string
	Tools         []Tool
	Stream        StreamFunc
	StreamOptions ai.SimpleStreamOptions
	Config        Config
	ToolExecution ToolExecutionMode
	SteeringMode  QueueMode
	FollowUpMode  QueueMode
}

type AgentOptions = Options

type Agent struct {
	model         ai.Model
	hasModel      bool
	systemPrompt  string
	sessionID     string
	thinkingLevel ai.ThinkingLevel
	tools         []Tool
	stream        StreamFunc
	streamOptions ai.SimpleStreamOptions
	config        Config
	toolExecution ToolExecutionMode
	steeringMode  QueueMode
	followUpMode  QueueMode
	steering      []Message
	followUp      []Message
	state         State
	activeCancel  context.CancelFunc
	aborted       bool
	activeDone    <-chan struct{}
	idleDone      chan struct{}
	listeners     []ActiveDoneListener
	mu            sync.Mutex
}

func New(options Options) *Agent {
	config := options.Config
	if config.ConvertToLLM == nil {
		config.ConvertToLLM = DefaultConvertToLLM
	}
	streamOptions := options.StreamOptions
	if isZeroSimpleStreamOptions(streamOptions) {
		streamOptions = config.SimpleOptions
	}
	agent := &Agent{model: options.Model, hasModel: !isZeroModel(options.Model), systemPrompt: options.SystemPrompt, sessionID: options.SessionID, tools: options.Tools, stream: options.Stream, streamOptions: copyStreamOptions(streamOptions), config: config, toolExecution: options.ToolExecution, steeringMode: options.SteeringMode, followUpMode: options.FollowUpMode}
	if options.InitialState != nil {
		state := copyState(*options.InitialState)
		state.Running = false
		state.IsStreaming = false
		state.StreamingMessage = nil
		state.PendingToolCalls = nil
		agent.state = state
		agent.systemPrompt = state.SystemPrompt
		if state.Model != nil {
			agent.model = *state.Model
			agent.hasModel = true
		}
		if state.ThinkingLevel != nil {
			agent.thinkingLevel = *state.ThinkingLevel
		}
		if state.Tools != nil {
			agent.tools = append([]Tool(nil), state.Tools...)
		}
	} else {
		agent.state = agent.currentState(nil, false)
	}
	return agent
}

func (agent *Agent) State() State {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return copyState(agent.state)
}

func (agent *Agent) ConvertToLLM(messages []Message) []ai.Message {
	if agent == nil || agent.config.ConvertToLLM == nil {
		return DefaultConvertToLLM(messages)
	}
	return agent.config.ConvertToLLM(messages)
}

func (agent *Agent) SetSystemPrompt(systemPrompt string) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.systemPrompt = systemPrompt
	agent.state.SystemPrompt = systemPrompt
}

func (agent *Agent) ReplaceTools(tools []Tool) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.tools = append([]Tool(nil), tools...)
	agent.state.Tools = append([]Tool(nil), tools...)
}

func (agent *Agent) SetModel(model ai.Model) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.model = model
	agent.hasModel = true
	agent.state.Model = copyModelPtr(model)
}

func (agent *Agent) SetThinkingLevel(level ai.ThinkingLevel) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.thinkingLevel = level
	agent.state.ThinkingLevel = copyThinkingLevelPtr(level)
}

func (agent *Agent) ReplaceState(state State) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.state = copyState(state)
	if state.Model != nil {
		agent.model = *state.Model
		agent.hasModel = true
	}
	if state.ThinkingLevel != nil {
		agent.thinkingLevel = *state.ThinkingLevel
	}
	agent.tools = append([]Tool(nil), state.Tools...)
	agent.systemPrompt = state.SystemPrompt
}

func (agent *Agent) IsStreaming() bool {
	return agent.State().Running
}

func (agent *Agent) Abort() {
	agent.mu.Lock()
	cancel := agent.activeCancel
	if cancel != nil {
		agent.aborted = true
	}
	agent.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (agent *Agent) ActiveDone() <-chan struct{} {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return agent.activeDone
}

func (agent *Agent) ActiveToken() <-chan struct{} {
	return agent.ActiveDone()
}

func (agent *Agent) WaitIdle(ctx context.Context) error {
	agent.mu.Lock()
	if !agent.state.Running {
		agent.mu.Unlock()
		return ctx.Err()
	}
	idleDone := agent.idleDone
	agent.mu.Unlock()
	select {
	case <-idleDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (agent *Agent) EnqueueSteering(message Message) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.steering = append(agent.steering, message)
}

func (agent *Agent) EnqueueFollowUp(message Message) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.followUp = append(agent.followUp, message)
}

func (agent *Agent) Subscribe(listener Listener) func() {
	return agent.SubscribeWithActiveDone(func(event Event, activeDone <-chan struct{}) {
		listener(event)
	})
}

func (agent *Agent) SubscribeWithActiveDone(listener ActiveDoneListener) func() {
	agent.mu.Lock()
	agent.listeners = append(agent.listeners, listener)
	agent.mu.Unlock()

	var once sync.Once
	listenerPointer := reflect.ValueOf(listener).Pointer()
	return func() {
		once.Do(func() {
			agent.mu.Lock()
			defer agent.mu.Unlock()
			for index, candidate := range agent.listeners {
				if reflect.ValueOf(candidate).Pointer() == listenerPointer {
					agent.listeners = append(agent.listeners[:index], agent.listeners[index+1:]...)
					return
				}
			}
		})
	}
}

func (agent *Agent) publish(event Event) {
	agent.mu.Lock()
	listeners := append([]ActiveDoneListener(nil), agent.listeners...)
	activeDone := agent.activeDone
	agent.mu.Unlock()
	for _, listener := range listeners {
		listener(event, activeDone)
	}
}

func (agent *Agent) Run(ctx context.Context, messages []Message) (State, error) {
	if state, ok := agent.guardNotStreaming(); !ok {
		return state, ErrAlreadyStreaming
	}
	return agent.run(ctx, messages, true)
}

func (agent *Agent) Prompt(ctx context.Context, message Message) (State, error) {
	return agent.PromptMany(ctx, []Message{message})
}

func (agent *Agent) PromptMany(ctx context.Context, messages []Message) (State, error) {
	return agent.Run(ctx, messages)
}

func (agent *Agent) Continue(ctx context.Context) (State, error) {
	if state, ok := agent.guardNotStreaming(); !ok {
		return state, ErrAlreadyStreaming
	}
	state := agent.State()
	if len(state.Messages) == 0 {
		return state, fmt.Errorf("No messages to continue from")
	}
	return agent.run(ctx, state.Messages, false)
}

func (agent *Agent) guardNotStreaming() (State, bool) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if !agent.state.Running {
		return copyState(agent.state), true
	}
	return copyState(agent.state), false
}

func (agent *Agent) beginRun(ctx context.Context) context.Context {
	runCtx, cancel := context.WithCancel(ctx)
	agent.mu.Lock()
	agent.aborted = false
	agent.activeCancel = cancel
	agent.activeDone = runCtx.Done()
	agent.idleDone = make(chan struct{})
	agent.mu.Unlock()
	return runCtx
}

func (agent *Agent) endRun() {
	agent.mu.Lock()
	cancel := agent.activeCancel
	idleDone := agent.idleDone
	agent.activeCancel = nil
	agent.activeDone = nil
	agent.idleDone = nil
	agent.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if idleDone != nil {
		close(idleDone)
	}
}

func (agent *Agent) run(ctx context.Context, messages []Message, publishInputMessages bool) (State, error) {
	ctx = agent.beginRun(ctx)
	defer agent.endRun()
	previous := agent.State()
	state := previous
	if publishInputMessages {
		state.Messages = append(append([]Message(nil), previous.Messages...), messages...)
	} else {
		state.Messages = append([]Message(nil), messages...)
	}
	state.Running = true
	state.IsStreaming = true
	state.StreamingMessage = nil
	state.PendingToolCalls = nil
	state.ErrorMessage = ""
	agent.setState(state)
	agent.publish(Event{Type: EventTypeStart})
	if publishInputMessages {
		startIndex := len(state.Messages) - len(messages)
		for index := startIndex; index < len(state.Messages); index++ {
			agent.publish(Event{Type: EventTypeMessageStart, Message: &state.Messages[index]})
			agent.publish(Event{Type: EventTypeMessageEnd, Message: &state.Messages[index]})
		}
	}
	stream := agent.stream
	if stream == nil {
		stream = func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			out := ai.NewAssistantMessageEventStream()
			out.Close(ai.DoneReasonStop)
			return out, nil
		}
	}

	for {
		if ctx.Err() != nil {
			break
		}
		agent.publish(Event{Type: EventTypeTurnStart})
		agentMessages := state.Messages
		if agent.config.TransformAgentContext != nil {
			transformed, err := agent.config.TransformAgentContext(ctx, agentMessages)
			if err != nil {
				state = state.withError(err)
				agent.setState(state)
				agent.publish(Event{Type: EventTypeError, Error: err})
				return state, err
			}
			agentMessages = transformed
		}
		llmMessages := agent.config.ConvertToLLM(agentMessages)
		if agent.config.TransformContext != nil {
			transformed, err := agent.config.TransformContext(ctx, llmMessages)
			if err != nil {
				state = state.withError(err)
				agent.setState(state)
				agent.publish(Event{Type: EventTypeError, Error: err})
				return state, err
			}
			llmMessages = transformed
		}
		if !agent.hasModel {
			err := fmt.Errorf("Agent has no model set; assign state.model first")
			state = state.withError(err)
			agent.setState(state)
			agent.publish(Event{Type: EventTypeError, Error: err})
			return state, err
		}
		if state.SystemPrompt != "" {
			systemMessage := ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: state.SystemPrompt}}}
			llmMessages = append([]ai.Message{systemMessage}, llmMessages...)
			agent.publish(Event{Type: EventTypeSystemPrompt, LLMMessage: &systemMessage})
		} else if len(llmMessages) > 0 && llmMessages[0].Role == ai.RoleSystem {
			agent.publish(Event{Type: EventTypeSystemPrompt, LLMMessage: &llmMessages[0]})
		}

		simpleOptions := copyStreamOptions(agent.streamOptions)
		baseOptions := simpleOptions.Base
		if baseOptions.SessionID == "" {
			baseOptions.SessionID = agent.sessionID
		}
		if agent.config.GetAPIKey != nil {
			if apiKey, ok := agent.config.GetAPIKey(ctx, agent.model.Provider); ok {
				baseOptions.APIKey = apiKey
			}
		}
		simpleOptions.Base = baseOptions
		if agent.thinkingLevel != "" {
			simpleOptions.ThinkingLevel = agent.thinkingLevel
		}
		events, err := stream(ctx, agent.model, llmMessages, ToolSpecs(agent.tools), simpleOptions)
		if err != nil {
			if err == context.Canceled && agent.wasAborted() {
				err = fmt.Errorf("aborted")
			}
			state = state.withError(err)
			agent.setState(state)
			agent.publish(Event{Type: EventTypeError, Error: err})
			return state, err
		}
		assistantMessage, err := agent.consumeAssistantStream(ctx, events, agent.model)
		if err != nil {
			if err == context.Canceled && agent.wasAborted() {
				err = fmt.Errorf("aborted")
			}
			state = state.withError(err)
			agent.setState(state)
			agent.publish(Event{Type: EventTypeError, Error: err})
			return state, err
		}

		llmAssistant := assistantLLMMessage(assistantMessage, agent.model)
		assistantAgentMessage := Message{Kind: MessageKindLLM, LLM: &llmAssistant}
		state.Messages = append(state.Messages, assistantAgentMessage)
		agent.setState(state)
		agent.publish(Event{Type: EventTypeAssistant, Message: &assistantAgentMessage, AssistantMessage: &assistantMessage})
		agent.publish(Event{Type: EventTypeMessageEnd, Message: &assistantAgentMessage})

		toolContext := agent.snapshotContext(ctx, state)
		toolResults := agent.executeToolCalls(ctx, assistantMessage, toolContext, assistantContentToolCalls(assistantMessage))
		turnToolResults := make([]ToolResult, 0, len(toolResults))
		toolResultCount := len(toolResults)
		allTerminate := toolResultCount > 0
		for _, outcome := range toolResults {
			result := outcome.Result
			turnToolResults = append(turnToolResults, result)
			agent.publish(Event{Type: EventTypeToolExecutionEnd, ToolCall: &outcome.Call, ToolResult: &result, IsError: result.IsError || result.Error != ""})
			resultMessage := NewToolResultMessage(result)
			state.Messages = append(state.Messages, resultMessage)
			agent.setState(state)
			agent.publish(Event{Type: EventTypeMessageStart, Message: &resultMessage})
			agent.publish(Event{Type: EventTypeToolResult, ToolCall: &outcome.Call, ToolResult: &result})
			agent.publish(Event{Type: EventTypeMessageEnd, Message: &resultMessage})
			if result.Terminate == nil || !*result.Terminate {
				allTerminate = false
			}
		}
		agent.publish(Event{Type: EventTypeTurnEnd, Message: &assistantAgentMessage, ToolResults: turnToolResults})
		turnContext := agent.turnHookContext(ctx, state, assistantMessage, turnToolResults)

		if agent.config.ShouldStopAfterTurn != nil {
			stop, err := agent.config.ShouldStopAfterTurn(ctx, turnContext)
			if err != nil {
				state = state.withError(err)
				agent.setState(state)
				agent.publish(Event{Type: EventTypeError, Error: err})
				return state, err
			}
			if stop {
				break
			}
		}

		if toolResultCount > 0 && allTerminate {
			break
		}

		nextCount := 0
		if agent.config.PrepareNextTurn != nil {
			update, err := agent.config.PrepareNextTurn(ctx, turnContext)
			if err != nil {
				state = state.withError(err)
				agent.setState(state)
				agent.publish(Event{Type: EventTypeError, Error: err})
				return state, err
			}
			if update != nil {
				if update.Context != nil {
					state.SystemPrompt = update.Context.SystemPrompt
					agent.systemPrompt = update.Context.SystemPrompt
					state.Messages = append([]Message(nil), update.Context.Messages...)
					agent.tools = append([]Tool(nil), update.Context.Tools...)
					state.Tools = append([]Tool(nil), agent.tools...)
				}
				if update.Model != nil {
					agent.model = *update.Model
					agent.hasModel = true
					state.Model = copyModelPtr(agent.model)
				}
				if update.SystemPrompt != nil {
					state.SystemPrompt = *update.SystemPrompt
					agent.systemPrompt = *update.SystemPrompt
				}
				if update.ThinkingLevel != nil {
					agent.thinkingLevel = *update.ThinkingLevel
					state.ThinkingLevel = copyThinkingLevelPtr(agent.thinkingLevel)
				}
				agent.setState(state)
				for _, message := range update.Messages {
					state.Messages = append(state.Messages, message)
					messageRef := &state.Messages[len(state.Messages)-1]
					agent.setState(state)
					agent.publish(Event{Type: EventTypeMessageStart, Message: messageRef})
					agent.publish(Event{Type: EventTypeMessageEnd, Message: messageRef})
				}
				nextCount += len(update.Messages)
			}
		}

		queued, err := agent.queuedMessages(ctx, assistantMessage.StopReason == ai.StopReasonToolCalls)
		if err != nil {
			state = state.withError(err)
			agent.setState(state)
			agent.publish(Event{Type: EventTypeError, Error: err})
			return state, err
		}
		if len(queued) > 0 {
			for _, message := range queued {
				state.Messages = append(state.Messages, message)
				messageRef := &state.Messages[len(state.Messages)-1]
				agent.setState(state)
				agent.publish(Event{Type: EventTypeMessageStart, Message: messageRef})
				agent.publish(Event{Type: EventTypeMessageEnd, Message: messageRef})
			}
			nextCount += len(queued)
		}

		if assistantMessage.StopReason != ai.StopReasonToolCalls && nextCount == 0 {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}

	state.Running = false
	state.IsStreaming = false
	state.Model = copyModelPtr(agent.model)
	state.ThinkingLevel = copyThinkingLevelPtr(agent.thinkingLevel)
	state.Tools = append([]Tool(nil), agent.tools...)
	agent.setState(state)
	agent.publish(Event{Type: EventTypeDone, Messages: append([]Message(nil), state.Messages...)})
	return state, nil
}

func (agent *Agent) wasAborted() bool {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return agent.aborted
}

func (agent *Agent) consumeAssistantStream(ctx context.Context, events *ai.AssistantMessageEventStream, model ai.Model) (ai.AssistantMessage, error) {
	partialStream := ai.NewAssistantMessageEventStream()
	started := false
	lastMessage := ai.AssistantMessage{}
	hasMessage := false
	defer agent.setStreamingMessage(nil)
	staticEventCount := len(events.SnapshotEvents())
	live := events.IsLive()
	if staticEventCount == 0 && !live {
		select {
		case <-ctx.Done():
			return ai.AssistantMessage{}, ctx.Err()
		default:
			return ai.AssistantMessage{}, fmt.Errorf("LLM stream produced no message")
		}
	}
	index := 0
	for {
		if !live && index >= staticEventCount && staticEventCount > 0 {
			break
		}
		streamEvent, nextIndex, err := events.Next(ctx, index)
		if err != nil {
			if hasMessage {
				return lastMessage, nil
			}
			return ai.AssistantMessage{}, err
		}
		index = nextIndex
		if streamEvent.Type == ai.EventError {
			return ai.AssistantMessage{}, fmt.Errorf("%s", streamEventErrorMessage(streamEvent))
		}
		if streamEvent.Type == ai.EventDone {
			partialStream.Emit(streamEvent)
			assistantMessage, completed := partialStream.Snapshot()
			if completed {
				return assistantMessage, nil
			}
			lastMessage = assistantMessage
			hasMessage = true
			break
		}
		if !isAssistantMessageUpdateEvent(streamEvent) {
			continue
		}
		partialStream.Emit(streamEvent)
		partial, _ := partialStream.Snapshot()
		lastMessage = partial
		hasMessage = true
		message := assistantMessageEventMessage(partial, model)
		if !started {
			agent.setStreamingMessage(&message)
			agent.publish(Event{Type: EventTypeMessageStart, Message: &message})
			started = true
		}
		streamEventCopy := streamEvent
		agent.setStreamingMessage(&message)
		agent.publish(Event{Type: EventTypeMessageUpdate, Message: &message, AssistantMessageEvent: &streamEventCopy})
	}
	if !hasMessage {
		return ai.AssistantMessage{}, fmt.Errorf("LLM stream produced no message")
	}
	return lastMessage, nil
}

func streamEventErrorMessage(event ai.AssistantMessageEvent) string {
	if event.Error != "" {
		return event.Error
	}
	if event.Message != nil && event.Message.ErrorMessage != "" {
		return event.Message.ErrorMessage
	}
	return "LLM stream error"
}

func isAssistantMessageUpdateEvent(event ai.AssistantMessageEvent) bool {
	switch event.Type {
	case ai.EventTextDelta, ai.EventThinkingDelta, ai.EventContentBlock, ai.EventContentUpdate, ai.EventToolCall, ai.EventToolCallDelta, ai.EventMetadata, ai.EventUsage:
		return true
	default:
		return false
	}
}

func assistantMessageEventMessage(assistant ai.AssistantMessage, model ai.Model) Message {
	llm := assistantLLMMessage(assistant, model)
	return Message{Kind: MessageKindLLM, LLM: &llm}
}

func assistantLLMMessage(assistant ai.AssistantMessage, model ai.Model) ai.Message {
	return ai.Message{Role: ai.RoleAssistant, API: model.API, Provider: model.Provider, Model: model.ID, ResponseModel: assistant.ResponseModel, ResponseID: assistant.ResponseID, Content: assistant.Content, ToolCalls: assistant.ToolCalls, Usage: assistant.Usage, StopReason: assistant.StopReason, ErrorMessage: assistant.ErrorMessage, Timestamp: assistant.Timestamp}
}

func assistantContentToolCalls(assistant ai.AssistantMessage) []ai.ToolCall {
	calls := make([]ai.ToolCall, 0, len(assistant.Content))
	for _, block := range assistant.Content {
		if block.Type == ai.ContentToolCall && block.ToolCall != nil {
			calls = append(calls, *block.ToolCall)
		}
	}
	return calls
}

func (agent *Agent) queuedMessages(ctx context.Context, modelContinues bool) ([]Message, error) {
	if messages := agent.drainSteering(); len(messages) > 0 {
		return messages, nil
	}
	if agent.config.GetSteeringMessages != nil {
		messages, err := agent.config.GetSteeringMessages(ctx)
		if err != nil {
			return nil, err
		}
		if len(messages) > 0 {
			return messages, nil
		}
	}
	if !modelContinues {
		if messages := agent.drainFollowUp(); len(messages) > 0 {
			return messages, nil
		}
		if agent.config.GetFollowUpMessages != nil {
			return agent.config.GetFollowUpMessages(ctx)
		}
	}
	return nil, nil
}

func (agent *Agent) drainSteering() []Message {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	messages, remaining := drainQueuedMessages(agent.steering, agent.steeringMode)
	agent.steering = remaining
	return append([]Message(nil), messages...)
}

func (agent *Agent) drainFollowUp() []Message {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	messages, remaining := drainQueuedMessages(agent.followUp, agent.followUpMode)
	agent.followUp = remaining
	return append([]Message(nil), messages...)
}

type PendingMessageQueue struct {
	mode  QueueMode
	items []Message
}

func NewPendingMessageQueue(mode QueueMode) *PendingMessageQueue {
	return &PendingMessageQueue{mode: mode}
}

func (queue *PendingMessageQueue) Enqueue(message Message) {
	if queue == nil {
		return
	}
	queue.items = append(queue.items, message)
}

func (queue *PendingMessageQueue) Drain() []Message {
	if queue == nil {
		return nil
	}
	if len(queue.items) == 0 {
		return nil
	}
	if normalizeQueueMode(queue.mode) == QueueOneAtATime {
		message := queue.items[0]
		queue.items = queue.items[1:]
		return []Message{message}
	}
	messages := append([]Message(nil), queue.items...)
	queue.items = nil
	return messages
}

func (queue *PendingMessageQueue) HasItems() bool {
	return queue != nil && len(queue.items) > 0
}

func drainQueuedMessages(messages []Message, mode QueueMode) ([]Message, []Message) {
	queue := PendingMessageQueue{mode: mode, items: append([]Message(nil), messages...)}
	drained := queue.Drain()
	return drained, queue.items
}

func normalizeQueueMode(mode QueueMode) QueueMode {
	switch mode {
	case QueueAll, "":
		return QueueAll
	case QueueOneAtATime:
		return QueueOneAtATime
	default:
		return QueueOneAtATime
	}
}

func (state State) withError(err error) State {
	state.Running = false
	state.IsStreaming = false
	state.StreamingMessage = nil
	state.PendingToolCalls = nil
	if err != nil {
		state.ErrorMessage = err.Error()
	}
	return state
}

func (agent *Agent) setState(state State) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.state = copyState(state)
}

func (agent *Agent) currentState(messages []Message, running bool) State {
	var model *ai.Model
	if agent.hasModel {
		model = copyModelPtr(agent.model)
	}
	return State{
		SystemPrompt:  agent.systemPrompt,
		Model:         model,
		ThinkingLevel: copyThinkingLevelPtr(agent.thinkingLevel),
		Tools:         append([]Tool(nil), agent.tools...),
		Messages:      append([]Message(nil), messages...),
		IsStreaming:   running,
		Running:       running,
	}
}

func isZeroModel(model ai.Model) bool {
	return model.ID == "" && model.Name == "" && model.API == "" && model.Provider == "" && model.BaseURL == "" && !model.Reasoning && len(model.Input) == 0 && model.Cost == nil && model.ContextWindow == 0 && model.MaxTokens == 0 && len(model.Headers) == 0 && len(model.ThinkingLevels) == 0 && len(model.Compat) == 0
}

func copyModelPtr(model ai.Model) *ai.Model {
	copyModel := model
	return &copyModel
}

func copyThinkingLevelPtr(level ai.ThinkingLevel) *ai.ThinkingLevel {
	if level == "" {
		return nil
	}
	copyLevel := level
	return &copyLevel
}

type toolOutcome struct {
	Call      ai.ToolCall
	ArgsValue any
	Result    ToolResult
	Error     error
}

type preparedToolCall struct {
	Call      ai.ToolCall
	ArgsValue any
	Result    *ToolResult
	Prompt    *ControlPlanePromptRequest
}

func (agent *Agent) snapshotContext(ctx context.Context, state State) AgentContext {
	return AgentContext{
		Context:         ctx,
		State:           copyState(state),
		SystemPrompt:    state.SystemPrompt,
		HasSystemPrompt: true,
		Messages:        append([]Message(nil), state.Messages...),
		Tools:           append([]Tool(nil), agent.tools...),
	}
}

func (agent *Agent) turnHookContext(ctx context.Context, state State, assistantMessage ai.AssistantMessage, toolResults []ToolResult) ShouldStopAfterTurnContext {
	agentContext := agent.snapshotContext(ctx, state)
	return ShouldStopAfterTurnContext{
		State:        copyState(state),
		Message:      assistantMessage,
		ToolResults:  append([]ToolResult(nil), toolResults...),
		AgentContext: agentContext,
		Context:      agentContext,
		NewMessages:  append([]Message(nil), state.Messages...),
	}
}

func copyState(state State) State {
	copyState := state
	copyState.IsStreaming = copyState.Running
	copyState.Model = nil
	if state.Model != nil {
		model := *state.Model
		copyState.Model = &model
	}
	copyState.ThinkingLevel = nil
	if state.ThinkingLevel != nil {
		level := *state.ThinkingLevel
		copyState.ThinkingLevel = &level
	}
	copyState.Tools = append([]Tool(nil), state.Tools...)
	copyState.Messages = append([]Message(nil), state.Messages...)
	copyState.StreamingMessage = nil
	if state.StreamingMessage != nil {
		message := *state.StreamingMessage
		copyState.StreamingMessage = &message
	}
	copyState.PendingToolCalls = append([]string(nil), state.PendingToolCalls...)
	return copyState
}

func (agent *Agent) setStreamingMessage(message *Message) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if message == nil {
		agent.state.StreamingMessage = nil
		return
	}
	copyMessage := *message
	agent.state.StreamingMessage = &copyMessage
}

func (agent *Agent) executeToolCalls(ctx context.Context, assistantMessage ai.AssistantMessage, agentContext AgentContext, calls []ai.ToolCall) []toolOutcome {
	prepared := make([]preparedToolCall, 0, len(calls))
	for _, call := range calls {
		preparedCall := agent.prepareToolCall(call)
		callCopy := preparedCall.Call
		agent.publish(Event{Type: EventTypeToolExecutionStart, ToolCall: &callCopy, ToolArgs: copyToolArgsValue(preparedCall.ArgsValue)})
		agent.publish(Event{Type: EventTypeToolCall, ToolCall: &callCopy})
		prepared = append(prepared, agent.prepareBeforeToolCall(ctx, preparedCall, assistantMessage, agentContext))
	}
	if agent.toolExecutionModeForCalls(calls) == ToolExecutionSequential {
		outcomes := make([]toolOutcome, 0, len(prepared))
		for _, preparedCall := range prepared {
			call := preparedCall.Call
			result, err := agent.executePendingToolCall(ctx, preparedCall, func(partial ToolResult) {
				partialCall := preparedCall.Call
				partial = normalizeToolUpdateResult(partial, partialCall)
				agent.publish(Event{Type: EventTypeToolUpdate, ToolCall: &partialCall, ToolArgs: copyToolArgsValue(preparedCall.ArgsValue), ToolResult: &partial})
			}, assistantMessage, agentContext)
			outcomes = append(outcomes, toolOutcome{Call: call, ArgsValue: copyToolArgsValue(preparedCall.ArgsValue), Result: result, Error: err})
		}
		return agent.applyAfterToolCalls(ctx, outcomes, assistantMessage, agentContext)
	}
	outcomes := make([]toolOutcome, len(prepared))
	var waitGroup sync.WaitGroup
	for index, preparedCall := range prepared {
		waitGroup.Add(1)
		go func(index int, preparedCall preparedToolCall) {
			defer waitGroup.Done()
			call := preparedCall.Call
			defer func() {
				if recovered := recover(); recovered != nil {
					outcomes[index] = toolOutcome{Call: call, ArgsValue: copyToolArgsValue(preparedCall.ArgsValue), Error: fmt.Errorf("tool task join: panic: %v", recovered)}
				}
			}()
			result, err := agent.executePendingToolCall(ctx, preparedCall, func(partial ToolResult) {
				partialCall := preparedCall.Call
				partial = normalizeToolUpdateResult(partial, partialCall)
				agent.publish(Event{Type: EventTypeToolUpdate, ToolCall: &partialCall, ToolArgs: copyToolArgsValue(preparedCall.ArgsValue), ToolResult: &partial})
			}, assistantMessage, agentContext)
			outcomes[index] = toolOutcome{Call: call, ArgsValue: copyToolArgsValue(preparedCall.ArgsValue), Result: result, Error: err}
		}(index, preparedCall)
	}
	waitGroup.Wait()
	return agent.applyAfterToolCalls(ctx, outcomes, assistantMessage, agentContext)
}

func (agent *Agent) applyAfterToolCalls(ctx context.Context, outcomes []toolOutcome, assistantMessage ai.AssistantMessage, agentContext AgentContext) []toolOutcome {
	for index := range outcomes {
		if outcomes[index].Error != nil {
			outcomes[index].Result = ToolResult{CallID: outcomes[index].Call.ID, Name: outcomes[index].Call.Name, Content: outcomes[index].Error.Error(), Error: outcomes[index].Error.Error(), IsError: true}
		}
		outcomes[index].Result = normalizeToolResult(outcomes[index].Result, outcomes[index].Call)
		result, err := agent.applyAfterToolCall(ctx, outcomes[index].Call, outcomes[index].ArgsValue, outcomes[index].Result, assistantMessage, agentContext)
		if err != nil {
			outcomes[index].Result.Error = err.Error()
			continue
		}
		outcomes[index].Result = result
	}
	return outcomes
}

func normalizeToolUpdateResult(result ToolResult, call ai.ToolCall) ToolResult {
	return normalizeToolResult(result, call)
}

func normalizeToolResult(result ToolResult, call ai.ToolCall) ToolResult {
	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
	if len(result.ContentBlocks) > 0 {
		result.ContentBlocks = toolResultContentBlocks(&result)
		result.Content = textFromContentBlocks(result.ContentBlocks)
	}
	if result.DetailsValue != nil {
		if details, ok := result.DetailsValue.(map[string]any); ok {
			result.Details = details
		} else {
			result.Details = nil
		}
	} else if result.Details != nil {
		result.DetailsValue = result.Details
	}
	return result
}

func (agent *Agent) addPendingToolCall(id string) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.state.PendingToolCalls = append(agent.state.PendingToolCalls, id)
}

func (agent *Agent) removePendingToolCall(id string) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	pending := agent.state.PendingToolCalls
	for index, candidate := range pending {
		if candidate == id {
			agent.state.PendingToolCalls = append(pending[:index], pending[index+1:]...)
			return
		}
	}
}

func (agent *Agent) executePendingToolCall(ctx context.Context, preparedCall preparedToolCall, update ToolUpdateFunc, assistantMessage ai.AssistantMessage, agentContext AgentContext) (ToolResult, error) {
	call := preparedCall.Call
	agent.addPendingToolCall(call.ID)
	defer agent.removePendingToolCall(call.ID)
	return agent.executeToolCall(ctx, preparedCall, update, assistantMessage, agentContext)
}

func (agent *Agent) toolExecutionModeForCalls(calls []ai.ToolCall) ToolExecutionMode {
	for _, call := range calls {
		for _, tool := range agent.tools {
			if tool.Name() != call.Name {
				continue
			}
			override, ok := tool.(ToolExecutionModeOverride)
			if ok && override.ExecutionMode() == ToolExecutionSequential {
				return ToolExecutionSequential
			}
		}
	}
	return agent.toolExecutionMode()
}

func (agent *Agent) prepareToolCall(call ai.ToolCall) preparedToolCall {
	prepared := preparedToolCall{Call: call, ArgsValue: copyToolArguments(call.Arguments)}
	for _, tool := range agent.tools {
		if tool.Name() != call.Name {
			continue
		}
		if valuePreparer, ok := tool.(ToolArgumentValuePreparer); ok {
			argsValue := valuePreparer.PrepareArgumentsValue(copyToolArguments(call.Arguments))
			prepared.ArgsValue = argsValue
			if argsMap, ok := argsValue.(map[string]any); ok {
				prepared.Call.Arguments = copyToolArguments(argsMap)
			} else {
				prepared.Call.Arguments = map[string]any{}
			}
			return prepared
		}
		preparer, ok := tool.(ToolArgumentPreparer)
		if !ok {
			return prepared
		}
		prepared.Call.Arguments = preparer.PrepareArguments(call.Arguments)
		prepared.ArgsValue = copyToolArguments(prepared.Call.Arguments)
		return prepared
	}
	return prepared
}

func copyToolArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	copyArguments := make(map[string]any, len(arguments))
	for key, value := range arguments {
		copyArguments[key] = value
	}
	return copyArguments
}

func copyToolArgsValue(arguments any) any {
	if argsMap, ok := arguments.(map[string]any); ok {
		return copyToolArguments(argsMap)
	}
	return arguments
}

func copyStreamOptions(options ai.SimpleStreamOptions) ai.SimpleStreamOptions {
	options.Base.Headers = copyStringMap(options.Base.Headers)
	options.Base.Metadata = copyAnyMap(options.Base.Metadata)
	options.Base.ProviderExtras = copyAnyMap(options.Base.ProviderExtras)
	return options
}

func isZeroSimpleStreamOptions(options ai.SimpleStreamOptions) bool {
	return options.Reasoning == "" && options.ThinkingLevel == "" && isZeroThinkingBudgets(options.ThinkingBudgets) && isZeroStreamOptions(options.Base)
}

func isZeroStreamOptions(options ai.StreamOptions) bool {
	return options.APIKey == "" && options.MaxTokens == 0 && options.Temperature == nil && options.Transport == "" && options.CacheRetention == "" && options.SessionID == "" && len(options.Headers) == 0 && options.TimeoutMS == 0 && options.MaxRetries == nil && options.MaxRetryDelayMS == nil && len(options.Metadata) == 0 && options.Abort == nil && len(options.ProviderExtras) == 0
}

func isZeroThinkingBudgets(budgets ai.ThinkingBudgets) bool {
	return budgets.Minimal == 0 && budgets.Low == 0 && budgets.Medium == 0 && budgets.High == 0
}

func copyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func copyAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	copyValues := make(map[string]any, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func (agent *Agent) toolExecutionMode() ToolExecutionMode {
	if agent.config.ToolExecution != "" {
		return normalizeToolExecutionMode(agent.config.ToolExecution)
	}
	if agent.toolExecution != "" {
		return normalizeToolExecutionMode(agent.toolExecution)
	}
	return ToolExecutionParallel
}

func normalizeToolExecutionMode(mode ToolExecutionMode) ToolExecutionMode {
	switch mode {
	case ToolExecutionSequential, ToolExecutionParallel:
		return mode
	default:
		return ToolExecutionSequential
	}
}

func (agent *Agent) prepareBeforeToolCall(ctx context.Context, preparedCall preparedToolCall, assistantMessage ai.AssistantMessage, agentContext AgentContext) preparedToolCall {
	call := preparedCall.Call
	var prompt *ControlPlanePromptRequest
	switch normalizePermissionClassification(agent.toolPermissionClassification(preparedCall)) {
	case PermissionBlock:
		reason := agent.toolPermissionReason(preparedCall, "tool call denied")
		result := ToolResult{CallID: call.ID, Name: call.Name, Content: reason, Error: reason, IsError: true}
		preparedCall.Result = &result
		return preparedCall
	case PermissionPrompt:
		request := agent.controlPlanePromptRequest(preparedCall)
		prompt = &request
	}

	if agent.config.BeforeToolCall != nil {
		decision, err := agent.config.BeforeToolCall(ctx, BeforeToolCallContext{AssistantMessage: assistantMessage, Call: call, ToolCall: call, Args: copyToolArgsValue(preparedCall.ArgsValue), AgentContext: agentContext, Context: agentContext})
		if err != nil {
			result := ToolResult{CallID: call.ID, Name: call.Name, Content: err.Error(), Error: err.Error(), IsError: true}
			preparedCall.Result = &result
			return preparedCall
		}
		if decision.Block {
			reason := decision.Reason
			if reason == "" {
				reason = "tool call blocked by before_tool_call hook"
			}
			result := ToolResult{CallID: call.ID, Name: call.Name, Content: reason, Error: reason, IsError: true}
			preparedCall.Result = &result
			return preparedCall
		}
		if decision.Skip {
			if decision.Result != nil {
				result := *decision.Result
				preparedCall.Result = &result
				return preparedCall
			}
			result := ToolResult{CallID: call.ID, Name: call.Name}
			preparedCall.Result = &result
			return preparedCall
		}
		if decision.Prompt != nil {
			if prompt != nil {
				prompt.Label = decision.Prompt.Label
				prompt.Payload = decision.Prompt.Payload
			} else {
				prompt = agent.bindControlPlanePrompt(preparedCall, decision.Prompt)
			}
		}
	}
	preparedCall.Prompt = prompt
	return preparedCall
}

func normalizePermissionClassification(classification PermissionClassification) PermissionClassification {
	switch classification {
	case PermissionAllow, PermissionPrompt, PermissionBlock:
		return classification
	default:
		return PermissionBlock
	}
}

func (agent *Agent) executeToolCall(ctx context.Context, preparedCall preparedToolCall, update ToolUpdateFunc, assistantMessage ai.AssistantMessage, agentContext AgentContext) (ToolResult, error) {
	call := preparedCall.Call
	if preparedCall.Result != nil {
		return *preparedCall.Result, nil
	}
	if preparedCall.Prompt != nil {
		resolution, err := agent.resolveControlPlanePrompt(ctx, *preparedCall.Prompt)
		if err != nil {
			return ToolResult{}, err
		}
		agent.publish(Event{Type: EventTypeControlPlanePromptResolved, ControlPlanePrompt: preparedCall.Prompt, ControlPlanePromptDecision: resolution.Decision, ControlPlanePromptReason: resolution.Reason})
		switch resolution.Decision {
		case ControlPlaneAllow:
		case ControlPlaneTimeout:
			return ToolResult{CallID: call.ID, Name: call.Name, Content: "control-plane prompt timed out — tool call denied", Error: "control-plane prompt timed out — tool call denied", IsError: true}, nil
		default:
			reason := resolution.Reason
			if reason == "" {
				reason = "tool call denied by user via control-plane prompt"
			}
			return ToolResult{CallID: call.ID, Name: call.Name, Content: reason, Error: reason, IsError: true}, nil
		}
	}

	for _, tool := range agent.tools {
		if tool.Name() == call.Name {
			result, err := tool.Execute(ctx, call, update)
			if result.CallID == "" {
				result.CallID = call.ID
			}
			if result.Name == "" {
				result.Name = call.Name
			}
			if err != nil {
				return result, err
			}
			return result, nil
		}
	}
	reason := fmt.Sprintf("No tool registered named '%s'", call.Name)
	return ToolResult{CallID: call.ID, Name: call.Name, Content: reason, Error: reason, IsError: true}, nil
}

func (agent *Agent) applyAfterToolCall(ctx context.Context, call ai.ToolCall, argsValue any, result ToolResult, assistantMessage ai.AssistantMessage, agentContext AgentContext) (ToolResult, error) {
	if agent.config.AfterToolCall == nil {
		return result, nil
	}
	patch, err := agent.config.AfterToolCall(ctx, AfterToolCallContext{AssistantMessage: assistantMessage, Call: call, ToolCall: call, Args: copyToolArgsValue(argsValue), Result: result, IsError: result.IsError || result.Error != "", AgentContext: agentContext, Context: agentContext})
	if err != nil {
		return result, err
	}
	if patch.Content != nil {
		result.Content = *patch.Content
		result.ContentBlocks = nil
	}
	if patch.ContentBlocksSet || len(patch.ContentBlocks) > 0 {
		result.ContentBlocks = userContentBlocks(patch.ContentBlocks)
		result.Content = textFromContentBlocks(result.ContentBlocks)
	}
	if patch.Details != nil {
		result.Details = patch.Details
		result.DetailsValue = patch.Details
	}
	if patch.DetailsValueSet || patch.DetailsValue != nil {
		result.DetailsValue = patch.DetailsValue
		if details, ok := patch.DetailsValue.(map[string]any); ok {
			result.Details = details
		} else {
			result.Details = nil
		}
	}
	if patch.IsError != nil {
		result.IsError = *patch.IsError
		if !*patch.IsError {
			result.Error = ""
		}
	}
	if patch.Terminate != nil {
		result.Terminate = patch.Terminate
	}
	return result, nil
}

func (agent *Agent) toolPermissionClassification(preparedCall preparedToolCall) PermissionClassification {
	call := preparedCall.Call
	for _, tool := range agent.tools {
		if tool.Name() != call.Name {
			continue
		}
		if classifier, ok := tool.(ToolPermissionValueClassifier); ok {
			return classifier.PermissionClassificationValue(preparedCall.ArgsValue)
		}
		classifier, ok := tool.(ToolPermissionClassifier)
		if !ok {
			return PermissionAllow
		}
		return classifier.PermissionClassification(call.Arguments)
	}
	return PermissionAllow
}

func (agent *Agent) toolPermissionReason(preparedCall preparedToolCall, fallback string) string {
	call := preparedCall.Call
	for _, tool := range agent.tools {
		if tool.Name() != call.Name {
			continue
		}
		if reasoner, ok := tool.(ToolPermissionValueReasoner); ok {
			if reason := reasoner.PermissionReasonValue(preparedCall.ArgsValue); reason != "" {
				return reason
			}
		}
		reasoner, ok := tool.(ToolPermissionReasoner)
		if ok {
			if reason := reasoner.PermissionReason(call.Arguments); reason != "" {
				return reason
			}
		}
	}
	return fallback
}

func (agent *Agent) toolPermissionReasonForCall(call ai.ToolCall, fallback string) string {
	return agent.toolPermissionReason(preparedToolCall{Call: call, ArgsValue: copyToolArguments(call.Arguments)}, fallback)
}

type controlPlanePromptResolution struct {
	Decision ControlPlanePromptDecision
	Reason   string
}

func (agent *Agent) resolveControlPlanePrompt(ctx context.Context, request ControlPlanePromptRequest) (controlPlanePromptResolution, error) {
	if agent.config.OnControlPlanePrompt == nil {
		return controlPlanePromptResolution{Decision: ControlPlaneDeny, Reason: "control-plane prompt required but no on_control_plane_prompt hook configured (fail-closed deny — see issue #110 design v0.2)"}, nil
	}
	reason := ""
	decision, err := agent.config.OnControlPlanePrompt(withControlPlanePromptReason(ctx, &reason), request)
	if decision != ControlPlaneAllow && decision != ControlPlaneDeny && decision != ControlPlaneTimeout {
		decision = ControlPlaneDeny
		if reason == "" {
			reason = "control-plane prompt returned unknown decision"
		}
	}
	return controlPlanePromptResolution{Decision: decision, Reason: reason}, err
}

func (agent *Agent) controlPlanePromptRequest(preparedCall preparedToolCall) ControlPlanePromptRequest {
	call := preparedCall.Call
	return ControlPlanePromptRequest{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		ArgsHash:   hashToolArgsValue(preparedCall.ArgsValue),
		Label:      "Control-plane write: " + call.Name,
		Payload:    defaultPromptPayloadValue(call.Name, preparedCall.ArgsValue),
		Reason:     agent.toolPermissionReason(preparedCall, "tool requires confirmation"),
	}
}

func defaultPromptPayload(call ai.ToolCall) map[string]any {
	return defaultPromptPayloadValue(call.Name, copyToolArguments(call.Arguments))
}

func defaultPromptPayloadValue(toolName string, argsValue any) map[string]any {
	const maxKeys = 32
	argsHash := hashToolArgsValue(argsValue)
	argsMap, _ := argsValue.(map[string]any)
	argumentKeys := make([]string, 0, len(argsMap))
	for key := range argsMap {
		argumentKeys = append(argumentKeys, key)
	}
	sort.Strings(argumentKeys)
	if len(argumentKeys) > maxKeys {
		argumentKeys = argumentKeys[:maxKeys]
	}
	keys := make([]string, 0, len(argumentKeys))
	for _, key := range argumentKeys {
		keys = append(keys, truncatePromptKey(key))
	}
	sort.Strings(keys)
	return map[string]any{"tool_name": toolName, "args_keys": keys, "args_hash": argsHash}
}

func truncatePromptKey(key string) string {
	const maxKeyRunes = 64
	runes := []rune(key)
	if len(runes) <= maxKeyRunes {
		return key
	}
	return string(runes[:maxKeyRunes]) + "…"
}

func (agent *Agent) bindControlPlanePrompt(preparedCall preparedToolCall, prompt *ControlPlanePromptRequest) *ControlPlanePromptRequest {
	call := preparedCall.Call
	bound := *prompt
	bound.ToolCallID = call.ID
	bound.ToolName = call.Name
	bound.ArgsHash = hashToolArgsValue(preparedCall.ArgsValue)
	return &bound
}

func hashToolArguments(arguments map[string]any) string {
	return hashToolArgsValue(arguments)
}

func hashToolArgsValue(arguments any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(arguments)
	if err != nil {
		buffer.Reset()
		buffer.WriteString("<args canonicalization failed>")
	} else {
		buffer.Truncate(buffer.Len() - 1)
	}
	sum := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(sum[:])
}
