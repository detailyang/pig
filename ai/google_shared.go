package ai

func IsGoogleThinkingPart(part map[string]any) bool {
	thought, _ := part["thought"].(bool)
	return thought
}

func IsThinkingPart(part map[string]any) bool {
	return IsGoogleThinkingPart(part)
}

func RetainGoogleThoughtSignature(existing string, incoming string) string {
	if incoming != "" {
		return incoming
	}
	return existing
}

func RetainThoughtSignature(existing string, incoming string) string {
	return RetainGoogleThoughtSignature(existing, incoming)
}

func MapGoogleStopReason(reason string) StopReason {
	switch reason {
	case "MAX_TOKENS":
		return StopReasonMaxTokens
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT":
		return StopReasonError
	default:
		return StopReasonEndTurn
	}
}

func MapStopReason(reason string) StopReason {
	return MapGoogleStopReason(reason)
}

func ConvertMessages(messages []Message) []map[string]any {
	return ConvertMessagesForGoogle(messages)
}

func ConvertTools(tools []Tool) []map[string]any {
	return ConvertToolsForGoogle(tools)
}
