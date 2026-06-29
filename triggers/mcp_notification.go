package triggers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/detailyang/pig/mcp"
)

func MapMCPNotification(serverName string, notification mcp.ServerNotification, receivedAt time.Time) (Trigger, bool) {
	params := normalizeMCPNotificationParams(notification.Params)
	idempotencyKey, replacementPolicy, ok := mcpNotificationIdempotency(serverName, notification.Method, params)
	if !ok {
		return Trigger{}, false
	}
	summary := renderMCPNotificationSummary(notification.Method, params)
	return Trigger{
		Source:            Source{Kind: SourceMCP, ServerName: serverName, Method: notification.Method},
		SourceKind:        SourceKindMCP,
		SourceLabel:       "mcp:" + serverName,
		EventLabel:        notification.Method,
		PayloadVisibility: PayloadLocal,
		PayloadSummary:    &summary,
		IDempotencyKey:    idempotencyKey,
		ReplacementPolicy: replacementPolicy,
		TraceID:           newMCPNotificationTraceID(),
		Authority: Authority{
			PrincipalID:     "mcp:" + serverName,
			PrincipalLabel:  serverName,
			CredentialScope: ScopeUser,
		},
		ReceivedAt: receivedAt,
	}, true
}

func normalizeMCPNotificationParams(params any) any {
	raw, ok := params.(json.RawMessage)
	if !ok {
		return params
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return params
	}
	return decoded
}

func mcpNotificationIdempotency(serverName, method string, params any) (string, ReplacementPolicy, bool) {
	prefix := "mcp:" + serverName + ":"
	switch method {
	case "notifications/tools/listChanged":
		return prefix + "tools", ReplacementLatestReplaces, true
	case "notifications/resources/listChanged":
		return prefix + "resources", ReplacementLatestReplaces, true
	case "notifications/prompts/listChanged":
		return prefix + "prompts", ReplacementLatestReplaces, true
	case "notifications/resources/updated":
		uri, ok := stringParam(params, "uri")
		if !ok {
			uri = "unknown"
		}
		return prefix + "resources:" + safeMCPIdempotencySegment(uri), ReplacementLatestReplaces, true
	default:
		dedupKey, ok := mcpDedupKey(params)
		if !ok {
			return "", "", false
		}
		return prefix + "custom:" + safeMCPIdempotencySegment(dedupKey), ReplacementDrop, true
	}
}

func mcpDedupKey(params any) (string, bool) {
	if value, ok := metaString(params, "pie_dedup_key"); ok {
		return value, true
	}
	return stringParam(params, "_pie_dedup_key")
}

func renderMCPNotificationSummary(method string, params any) string {
	switch method {
	case "notifications/resources/updated":
		if uri, ok := stringParam(params, "uri"); ok {
			return fmt.Sprintf("%s uri=%s", method, safeMCPDisplay(uri, 200))
		}
		return method
	case "notifications/tools/listChanged", "notifications/resources/listChanged", "notifications/prompts/listChanged":
		return method
	default:
		if summary, ok := metaString(params, "pie_summary"); ok {
			return fmt.Sprintf("%s %s", method, safeMCPDisplay(summary, 200))
		}
		return method
	}
}

func safeMCPIdempotencySegment(value string) string {
	redacted := redactMCPNotificationText(value)
	if redacted != value || utf8.RuneCountInString(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		digest := sha256.Sum256([]byte(value))
		return "hash:" + hex.EncodeToString(digest[:6])
	}
	return value
}

func SafeIdempotencySegment(value string) string {
	return safeMCPIdempotencySegment(value)
}

func safeMCPDisplay(value string, cap int) string {
	redacted := strings.ReplaceAll(redactMCPNotificationText(value), "\n", " ")
	return truncateMCPChars(redacted, cap)
}

func SafeDisplay(value string, cap int) string {
	return safeMCPDisplay(value, cap)
}

func redactMCPNotificationText(value string) string {
	parts := strings.Fields(value)
	for index, part := range parts {
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "hub_agent_") || strings.HasPrefix(lower, "hub_hs_") || strings.HasPrefix(lower, "hub_ep_") || strings.HasPrefix(lower, "sk-") || strings.Contains(lower, "bearer") || strings.Contains(lower, "token") {
			parts[index] = "[redacted]"
		}
	}
	return strings.Join(parts, " ")
}

func truncateMCPChars(value string, cap int) string {
	if cap <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= cap {
		return value
	}
	runes := []rune(value)
	return string(runes[:cap-1]) + "…"
}

func stringParam(params any, key string) (string, bool) {
	paramsMap, ok := params.(map[string]any)
	if !ok {
		return "", false
	}
	value, ok := paramsMap[key].(string)
	return value, ok
}

func metaString(params any, key string) (string, bool) {
	paramsMap, ok := params.(map[string]any)
	if !ok {
		return "", false
	}
	meta, ok := paramsMap["_meta"].(map[string]any)
	if !ok {
		return "", false
	}
	value, ok := meta[key].(string)
	return value, ok
}

func newMCPNotificationTraceID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("mcp-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
