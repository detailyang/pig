package debug

import (
	"time"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/debuglog"
)

const PreviewMaxChars = debuglog.PreviewMaxChars
const PreviewMaxLines = debuglog.PreviewMaxLines

const DEBUG_PREVIEW_MAX_CHARS = debuglog.DEBUG_PREVIEW_MAX_CHARS
const DEBUG_PREVIEW_MAX_LINES = debuglog.DEBUG_PREVIEW_MAX_LINES

type Options = debuglog.Options

type StreamFunc = debuglog.StreamFunc

func WrapStreamFunc(base StreamFunc, emit func(string)) StreamFunc {
	return debuglog.WrapStreamFunc(base, emit)
}

func WrapStreamFn(base StreamFunc, emit func(string)) StreamFunc {
	return debuglog.WrapStreamFn(base, emit)
}

func StartLine(callID uint64, model ai.Model, context ai.Context, options Options) string {
	return debuglog.StartLine(callID, model, context, options)
}

func ContextLine(callID uint64, context ai.Context) (string, bool) {
	return debuglog.ContextLine(callID, context)
}

func ToolCallLine(callID uint64, toolCall ai.ToolCall) string {
	return debuglog.ToolCallLine(callID, toolCall)
}

func DoneLine(callID uint64, reason ai.DoneReason, message ai.AssistantMessage, elapsed time.Duration) string {
	return debuglog.DoneLine(callID, reason, message, elapsed)
}

func Preview(text string) string {
	return debuglog.Preview(text)
}

func DebugPreview(text string) string {
	return debuglog.DebugPreview(text)
}

func ElapsedMS(elapsed time.Duration) string {
	return debuglog.ElapsedMS(elapsed)
}

func Emit(emit func(string), text string) {
	debuglog.Emit(emit, text)
}

func UserContentBlockLog(block ai.UserContentBlock) string {
	return debuglog.UserContentBlockLog(block)
}

func UserContentLog(content ai.UserContent) string {
	return debuglog.UserContentLog(content)
}
