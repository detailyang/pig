package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/detailyang/pig/ai"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/detailyang/pig/agent"

// OpenTelemetryOptions enables tracing through an application-configured
// OpenTelemetry SDK. Inputs and outputs are excluded unless explicitly enabled.
type OpenTelemetryOptions struct {
	TracerProvider trace.TracerProvider
	RecordInputs   bool
	RecordOutputs  bool
}

type openTelemetry struct {
	tracer        trace.Tracer
	recordInputs  bool
	recordOutputs bool
}

func newOpenTelemetry(options *OpenTelemetryOptions) *openTelemetry {
	if options == nil {
		return nil
	}
	provider := options.TracerProvider
	if provider == nil {
		provider = otel.GetTracerProvider()
	}
	return &openTelemetry{
		tracer:        provider.Tracer(instrumentationName),
		recordInputs:  options.RecordInputs,
		recordOutputs: options.RecordOutputs,
	}
}

func (telemetry *openTelemetry) startAgent(ctx context.Context, model ai.Model) (context.Context, trace.Span) {
	if telemetry == nil {
		return ctx, nil
	}
	return telemetry.tracer.Start(ctx, "invoke_agent", trace.WithAttributes(
		attribute.String("gen_ai.operation.name", "invoke_agent"),
		attribute.String("gen_ai.provider.name", string(model.Provider)),
		attribute.String("gen_ai.request.model", model.ID),
	))
}

func (telemetry *openTelemetry) startModel(ctx context.Context, model ai.Model, messages []ai.Message) (context.Context, trace.Span) {
	if telemetry == nil {
		return ctx, nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.provider.name", string(model.Provider)),
		attribute.String("gen_ai.request.model", model.ID),
	}
	if telemetry.recordInputs {
		attrs = appendJSONAttribute(attrs, "gen_ai.input.messages", messages)
	}
	return telemetry.tracer.Start(ctx, "chat "+model.ID, trace.WithAttributes(attrs...))
}

func (telemetry *openTelemetry) endModel(span trace.Span, message ai.AssistantMessage, err error) {
	if span == nil {
		return
	}
	if message.ResponseID != "" {
		span.SetAttributes(attribute.String("gen_ai.response.id", message.ResponseID))
	}
	if usage := message.Usage; usage != nil {
		input := usage.InputTokens
		if input == 0 {
			input = usage.Input
		}
		output := usage.OutputTokens
		if output == 0 {
			output = usage.Output
		}
		span.SetAttributes(
			attribute.Int("gen_ai.usage.input_tokens", input),
			attribute.Int("gen_ai.usage.output_tokens", output),
		)
	}
	if telemetry.recordOutputs {
		span.SetAttributes(jsonAttribute("gen_ai.output.messages", message)...)
	}
	telemetry.endSpan(span, err)
}

func (telemetry *openTelemetry) startTool(ctx context.Context, call ai.ToolCall, args any) (context.Context, trace.Span) {
	if telemetry == nil {
		return ctx, nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "execute_tool"),
		attribute.String("gen_ai.tool.name", call.Name),
		attribute.String("gen_ai.tool.call.id", call.ID),
	}
	if telemetry.recordInputs {
		attrs = appendJSONAttribute(attrs, "gen_ai.tool.call.arguments", args)
	}
	return telemetry.tracer.Start(ctx, "execute_tool "+call.Name, trace.WithAttributes(attrs...))
}

func (telemetry *openTelemetry) endTool(span trace.Span, result ToolResult, err error) {
	if span == nil {
		return
	}
	if telemetry.recordOutputs {
		span.SetAttributes(jsonAttribute("gen_ai.tool.call.result", result)...)
	}
	if err == nil && result.Error != "" {
		err = errors.New(result.Error)
	} else if err == nil && result.IsError {
		err = errors.New("tool execution failed")
	}
	telemetry.endSpan(span, err)
}

func (telemetry *openTelemetry) endSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func appendJSONAttribute(attrs []attribute.KeyValue, key string, value any) []attribute.KeyValue {
	return append(attrs, jsonAttribute(key, value)...)
}

func jsonAttribute(key string, value any) []attribute.KeyValue {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return []attribute.KeyValue{attribute.String(key, string(data))}
}
