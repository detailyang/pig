package sessionexport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
	"github.com/detailyang/pig/session"
)

func RenderEntries(entries []session.Entry) string {
	return RenderContext(session.BuildSessionContext(entries))
}

func Render(sess *session.Session) (string, error) {
	ctx, err := sess.BuildContext()
	if err != nil {
		return "", err
	}
	return RenderContext(ctx), nil
}

func RenderContext(ctx session.Context) string {
	var out strings.Builder
	out.WriteString("# Session Transcript\n\n")
	if ctx.Model != nil {
		out.WriteString(fmt.Sprintf("- Model: `%s:%s`\n", ctx.Model.Provider, ctx.Model.ModelID))
	}
	out.WriteString(fmt.Sprintf("- Thinking level: `%s`\n", ctx.ThinkingLevel))
	out.WriteString(fmt.Sprintf("- Messages: %d\n\n", len(ctx.Messages)))
	for index, message := range ctx.Messages {
		renderMessage(&out, index, message)
	}
	return out.String()
}

func DefaultExportPath(sessionID string) string {
	return filepath.Join(config.BaseDir(), "exports", sessionID+".md")
}

func SaveContext(ctx session.Context, dest string) (string, error) {
	if parent := filepath.Dir(dest); parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", fmt.Errorf("create exports dir %s: %w", parent, err)
		}
	}
	if err := os.WriteFile(dest, []byte(RenderContext(ctx)), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", dest, err)
	}
	return dest, nil
}

func Save(sess *session.Session, dest string) (string, error) {
	ctx, err := sess.BuildContext()
	if err != nil {
		return "", err
	}
	return SaveContext(ctx, dest)
}

func renderMessage(out *strings.Builder, index int, message agent.Message) {
	switch message.Kind {
	case agent.MessageKindLLM:
		if message.LLM != nil {
			renderLLMMessage(out, index, *message.LLM)
		}
	case agent.MessageKindToolResult:
		if message.ToolResult != nil {
			out.WriteString(fmt.Sprintf("### tool result `%s`\n\n", message.ToolResult.CallID))
			if len(message.ToolResult.ContentBlocks) > 0 {
				out.WriteString(renderContent(message.ToolResult.ContentBlocks))
			} else {
				out.WriteString(message.ToolResult.Content)
			}
			out.WriteString("\n\n")
		}
	case agent.MessageKindCustom:
		if message.Custom != nil {
			data, _ := marshalJSONIndentNoHTMLEscape(message.Custom.Payload)
			out.WriteString(fmt.Sprintf("### custom: %s\n\n```json\n%s\n```\n\n", message.Custom.Role, string(data)))
		}
	}
}

func renderLLMMessage(out *strings.Builder, index int, message ai.Message) {
	switch message.Role {
	case ai.RoleUser:
		out.WriteString(fmt.Sprintf("## %d. User\n\n", index))
		out.WriteString(renderContent(message.Content))
		out.WriteString("\n\n")
	case ai.RoleAssistant:
		out.WriteString(fmt.Sprintf("## %d. Assistant\n\n", index))
		for _, block := range message.Content {
			switch block.Type {
			case ai.ContentText:
				out.WriteString(block.Text)
				out.WriteString("\n\n")
			case ai.ContentThinking:
				out.WriteString("<details><summary>thinking</summary>\n\n")
				out.WriteString(fmt.Sprintf("```\n%s\n```\n", block.Thinking))
				out.WriteString("\n</details>\n\n")
			case ai.ContentToolCall:
				if block.ToolCall == nil {
					continue
				}
					args, _ := marshalJSONNoHTMLEscape(block.ToolCall.Arguments)
				out.WriteString(fmt.Sprintf("**tool call** `%s` `%s`:\n```json\n%s\n```\n\n", block.ToolCall.Name, block.ToolCall.ID, string(args)))
			case ai.ContentImage:
				out.WriteString("`[image]`\n\n")
			}
		}
	case ai.RoleTool:
		out.WriteString(fmt.Sprintf("### tool result `%s`\n\n", message.ToolCallID))
		out.WriteString(renderContent(message.Content))
		out.WriteString("\n\n")
	}
}

func renderContent(blocks []ai.ContentBlock) string {
	return RenderUserContent(ai.UserContentBlocksValue(userContentBlocks(blocks)))
}

func RenderUserContent(content ai.UserContent) string {
	if content.Blocks == nil {
		return content.Text
	}
	parts := make([]string, 0, len(content.Blocks))
	for _, block := range content.Blocks {
		switch block.Type {
		case ai.UserContentText:
			parts = append(parts, block.Text)
		case ai.UserContentImage:
			parts = append(parts, "`[image]`")
		}
	}
	return strings.Join(parts, "\n\n")
}

func userContentBlocks(blocks []ai.ContentBlock) []ai.UserContentBlock {
	parts := make([]ai.UserContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ai.ContentText:
			parts = append(parts, ai.UserContentBlock{Type: ai.UserContentText, Text: block.Text})
		case ai.ContentImage:
			parts = append(parts, ai.UserContentBlock{Type: ai.UserContentImage, Data: block.Data, MimeType: block.MimeType})
		}
	}
	return parts
}
